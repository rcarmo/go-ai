package durable

import (
	"context"
	"errors"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type mainRecoveryStream struct {
	events chan goai.Event
	once   sync.Once
}

func (s *mainRecoveryStream) close() { s.once.Do(func() { close(s.events) }) }

type mainRecoveryTimer struct {
	due       int64
	fire      func()
	cancelled bool
}
type mainRecoveryHarness struct {
	t          *testing.T
	h          *Harness
	c          *ConversationHandle
	store      Storage
	frames     <-chan upstreamWindowFrame
	clock      *atomic.Int64
	timerMu    sync.Mutex
	timers     []*mainRecoveryTimer
	replicaMu  sync.Mutex
	replica    JSON
	replicaSeq uint64
	replicaErr error
	replicated chan struct{}
	memory     []string
}

func mainOpenRecovery(t *testing.T, store Storage, ref ModelRef, options Options, clock *atomic.Int64, window float64) *mainRecoveryHarness {
	t.Helper()
	f := &mainRecoveryHarness{t: t, store: store, clock: clock, replicated: make(chan struct{}, 128)}
	zero := int64(0)
	options.Settings = &HarnessSettings{Progress: &ProgressSettings{PartialIntervalMs: &zero, PersistWithinMs: &window}}
	options.Now = clock.Load
	options.SetTimer = func(fire func(), delay time.Duration, opts TimerOptions) func() {
		if delay <= 0 {
			fire()
			return func() {}
		}
		timer := &mainRecoveryTimer{due: clock.Load() + delay.Milliseconds(), fire: fire}
		if !opts.Background {
			f.timerMu.Lock()
			f.timers = append(f.timers, timer)
			f.timerMu.Unlock()
		}
		return func() { f.timerMu.Lock(); timer.cancelled = true; f.timerMu.Unlock() }
	}
	f.h = openHarness(t, store, options)
	f.c = root(t, f.h, ref)
	_, f.frames = recordUpstreamWindow(t, f.c)
	base, sub, err := f.h.session.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	f.replica = f.live(base)
	f.replicaSeq = base.Seq
	if err := sub.Start(func(_ context.Context, frame PublicationFrame) error {
		f.replicaMu.Lock()
		defer f.replicaMu.Unlock()
		if frame.Snapshot != nil {
			f.replicaErr = errors.New("unexpected resnapshot instead of stored publication")
		}
		for _, change := range frame.Publication.Documents {
			if change.Record.Kind == "pi.live" && change.Record.Owner == f.c.ID() {
				next, err := ApplyOperations(f.replica, change.Ops, f.h.session.limits)
				if err != nil {
					f.replicaErr = err
				} else {
					switch value := next.(type) {
					case JSON:
						f.replica = value
					case map[string]any:
						f.replica = JSON(value)
					default:
						f.replicaErr = errors.New("publication changed live document to non-object")
					}
				}
			}
		}
		if frame.Publication.Seq > f.replicaSeq {
			f.replicaSeq = frame.Publication.Seq
		}
		select {
		case f.replicated <- struct{}{}:
		default:
		}
		return f.replicaErr
	}); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *mainRecoveryHarness) live(snapshot Snapshot) JSON {
	for _, doc := range snapshot.Documents {
		if doc.Kind == "pi.live" && doc.Owner == f.c.ID() {
			return doc.Value
		}
	}
	return JSON{}
}
func (f *mainRecoveryHarness) stored() Snapshot {
	f.t.Helper()
	state, err := f.store.Snapshot(bg)
	if err != nil {
		f.t.Fatal(err)
	}
	return state
}
func (f *mainRecoveryHarness) partial(stream *mainRecoveryStream, text string) {
	f.t.Helper()
	now := f.clock.Add(100)
	f.timerMu.Lock()
	var jobs []func()
	for _, timer := range f.timers {
		if !timer.cancelled && timer.due <= now {
			timer.cancelled = true
			jobs = append(jobs, timer.fire)
		}
	}
	f.timerMu.Unlock()
	for _, job := range jobs {
		job()
	}
	stream.events <- &goai.TextDeltaEvent{Partial: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: text}}}}
	for {
		frame := watchReceive(f.t, f.frames)
		if value, ok := mainRecoveryText(frame.value.Docs["pi.live"]); ok {
			f.memory = append(f.memory, value)
			if value == text {
				return
			}
		}
	}
}
func mainRecoveryText(live JSON) (string, bool) {
	generation, _ := live["generation"].(map[string]any)
	message, _ := generation["message"].(map[string]any)
	content, _ := message["content"].([]any)
	for _, value := range content {
		if block, ok := value.(map[string]any); ok && block["type"] == "text" {
			text, ok := block["text"].(string)
			return text, ok
		}
	}
	return "", false
}
func (f *mainRecoveryHarness) replicaMatches(state Snapshot) {
	f.t.Helper()
	for {
		f.replicaMu.Lock()
		seq, err := f.replicaSeq, f.replicaErr
		replica := f.replica
		f.replicaMu.Unlock()
		if err != nil {
			f.t.Fatal(err)
		}
		if seq >= state.Seq {
			if !equalJSONValue(replica, f.live(state)) {
				f.t.Fatal("stored-publication replica differs from Storage", replica, f.live(state))
			}
			return
		}
		watchReceive(f.t, f.replicated)
	}
}
func (f *mainRecoveryHarness) crash() Storage {
	f.t.Helper()
	_ = f.h.session.backendResult(bg, errors.New("simulated process loss"), true)
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if end, err := f.h.session.Closed(ctx); err != nil || end.Reason != "failed" {
		f.t.Fatal("crash end", end, err)
	}
	// Join the native host before reopening; cancellation is not a terminal reply.
	_ = f.h.Close(ctx)
	return utf16ReopenStore(f.t, f.store)
}
func mainRecoverySetup(t *testing.T) (ModelRef, Options, chan *mainRecoveryStream, *atomic.Int64) {
	streams := make(chan *mainRecoveryStream, 8)
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		stream := &mainRecoveryStream{events: make(chan goai.Event, 1)}
		streams <- stream
		go func() { <-ctx.Done(); stream.close() }()
		return stream.events
	})
	clock := new(atomic.Int64)
	clock.Store(1000)
	return ref, options, streams, clock
}
func mainFinishRecovery(t *testing.T, f *mainRecoveryHarness, streams <-chan *mainRecoveryStream, submission ID, want []JSON) {
	t.Helper()
	if err := f.h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	stream := watchReceive(t, streams)
	stream.events <- terminal("answer")
	stream.close()
	sub, err := f.h.Submission(bg, submission)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitSubmission(t, sub); got.Submission.Status != "done" {
		t.Fatal(got)
	}
	if err := f.h.WaitForIdle(bg); err != nil {
		t.Fatal(err)
	}
	state, err := f.h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var answers []JSON
	for _, id := range ids(state.Entries) {
		entry := state.Entries[id]
		if entry.Conversation != f.c.ID() || entry.Value["role"] != string(goai.RoleAssistant) {
			continue
		}
		var message MessageReceipt
		if err := fromObject(entry.Value, &message, f.h.session.limits); err != nil {
			t.Fatal(err)
		}
		answers = append(answers, JSON{"text": message.Content[0].Text, "stopReason": string(message.StopReason)})
	}
	if !reflect.DeepEqual(answers, want) {
		t.Errorf("recovered answers=%v want=%v", answers, want)
	}
	if live := f.live(state); len(live) != 0 {
		t.Error("successful generation retained live presentation", live)
	}
	if err := f.h.Close(bg); err != nil {
		t.Fatal(err)
	}
}

// f9a26365 harness-deferred-progress.test.ts:116-156, with controlled provider
// clocks and native failed-Session/reopen adapters instead of process killing.
func TestMainHarnessRecoveryStoredPartialAndReplica(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		ref, options, streams, clock := mainRecoverySetup(t)
		f := mainOpenRecovery(t, b.store, ref, options, clock, 150)
		sub, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream := watchReceive(t, streams)
		f.partial(stream, strings.Repeat("a", 64))
		f.partial(stream, strings.Repeat("a", 96))
		state := f.stored()
		f.replicaMatches(state)
		stored, ok := mainRecoveryText(f.live(state))
		if !ok || len(stored) <= 40 {
			t.Fatal("stored partial absent", stored)
		}
		if !strings.HasPrefix(f.memory[len(f.memory)-1], stored) {
			t.Error("Storage is not an earlier memory prefix")
		}
		seen := false
		for _, value := range f.memory {
			if value == stored {
				seen = true
			}
		}
		if !seen {
			t.Error("stored value never observed in memory")
		}
		store := f.crash()
		f = mainOpenRecovery(t, store, ref, options, clock, 150)
		mainFinishRecovery(t, f, streams, sub.ID(), []JSON{{"text": stored, "stopReason": "aborted"}, {"text": "answer", "stopReason": "stop"}})
	})
}

// Source158-175: an unstored attempt and partial must not become an entry.
func TestMainHarnessRecoveryBeforeFirstStore(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		ref, options, streams, clock := mainRecoverySetup(t)
		f := mainOpenRecovery(t, b.store, ref, options, clock, math.Inf(1))
		sub, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream := watchReceive(t, streams)
		f.partial(stream, strings.Repeat("b", 64))
		store := f.crash()
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if value, exists := f.live(state)["generation"]; exists {
			t.Error("unstored attempt presentation survived crash", value)
		}
		if text, ok := mainRecoveryText(f.live(state)); ok {
			t.Error("unstored partial survived crash", text)
		}
		f = mainOpenRecovery(t, store, ref, options, clock, math.Inf(1))
		mainFinishRecovery(t, f, streams, sub.ID(), []JSON{{"text": "answer", "stopReason": "stop"}})
	})
}

// Source177-207: crash before conversion and during the resend must retain one
// aborted stored partial, with no duplicate from the held resend.
func TestMainHarnessRecoveryRepeatedCrashes(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		ref, options, streams, clock := mainRecoverySetup(t)
		f := mainOpenRecovery(t, b.store, ref, options, clock, 150)
		sub, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream := watchReceive(t, streams)
		f.partial(stream, strings.Repeat("c", 64))
		f.partial(stream, strings.Repeat("c", 96))
		stored, ok := mainRecoveryText(f.live(f.stored()))
		if !ok {
			t.Fatal("missing stored partial")
		}
		store := f.crash()
		f = mainOpenRecovery(t, store, ref, options, clock, 150)
		store = f.crash()
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := mainRecoveryText(f.live(state)); !ok || text != stored {
			t.Error("crash before conversion lost partial", text, stored)
		}
		f = mainOpenRecovery(t, store, ref, options, clock, math.Inf(1))
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		stream = watchReceive(t, streams)
		f.partial(stream, strings.Repeat("d", 64))
		store = f.crash()
		state, err = store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := mainRecoveryText(f.live(state)); ok {
			t.Error("resend's held partial survived crash", text)
		}
		f = mainOpenRecovery(t, store, ref, options, clock, math.Inf(1))
		mainFinishRecovery(t, f, streams, sub.ID(), []JSON{{"text": stored, "stopReason": "aborted"}, {"text": "answer", "stopReason": "stop"}})
	})
}

func TestMainGenerationPartialLegacyAndOwnerFence(t *testing.T) {
	message := messageReceipt{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "stored"}}}
	value, err := dtoObject(message, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: 7, Conversation: 3}
	live := Document{ID: 9, Kind: "pi.live", Owner: 3, Value: JSON{"run": JSON{"task": 7}, "generation": JSON{"message": value}}}
	state := Snapshot{Documents: map[ID]Document{9: live}}
	got, err := generationPartial(state, task, generationCheckpoint{}, DefaultLimits())
	if err != nil || got == nil || got.Content[0].Text != "stored" {
		t.Fatal(got, err)
	}
	legacy := message
	legacy.Content = []goai.ContentBlock{{Type: "text", Text: "legacy checkpoint"}}
	got, err = generationPartial(state, task, generationCheckpoint{Partial: &legacy}, DefaultLimits())
	if err != nil || got == nil || got.Content[0].Text != "legacy checkpoint" {
		t.Fatal("legacy receipt changed", got, err)
	}
	for _, mode := range []string{"other-task", "other-owner", "retired", "cleared"} {
		t.Run(mode, func(t *testing.T) {
			copy := live
			switch mode {
			case "other-task":
				copy.Value = JSON{"run": JSON{"task": 8}, "generation": JSON{"message": value}}
			case "other-owner":
				copy.Owner = 4
			case "retired":
				copy.Retired = true
			case "cleared":
				copy.Value = JSON{"run": JSON{"task": 7}}
			}
			got, err := generationPartial(Snapshot{Documents: map[ID]Document{9: copy}}, task, generationCheckpoint{}, DefaultLimits())
			if err != nil || got != nil {
				t.Fatal("foreign/cleared partial adopted", got, err)
			}
		})
	}
}

// Native fault boundary derived from Source177: conversion and removal of its
// stored presentation must be one transaction, before another intent can store.
func TestMainHarnessRecoveryCrashAfterConversion(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		ref, options, streams, clock := mainRecoverySetup(t)
		f := mainOpenRecovery(t, b.store, ref, options, clock, 150)
		sub, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream := watchReceive(t, streams)
		f.partial(stream, strings.Repeat("e", 64))
		f.partial(stream, strings.Repeat("e", 96))
		stored, ok := mainRecoveryText(f.live(f.stored()))
		if !ok {
			t.Fatal("missing stored partial")
		}
		store := f.crash()
		f = mainOpenRecovery(t, store, ref, options, clock, math.Inf(1))
		core := f.h.session.core
		original := core.appendFrame
		converted := false
		installTaskAppend(t, f.h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 && converted {
				return errors.New("process loss after conversion before next intent")
			}
			converts := false
			if kind == 2 {
				var commit commitRecord
				if err := decodeStrict(payload, f.h.session.limits, f.h.session.limits.MaxFramePayloadBytes, &commit); err != nil {
					return err
				}
				for _, write := range commit.Writes {
					if write.Entry != nil && write.Entry.Value["role"] == string(goai.RoleAssistant) && write.Entry.Value["stopReason"] == string(goai.StopReasonAborted) {
						converts = true
					}
				}
			}
			if original != nil {
				if err := original(kind, ordinal, high, payload); err != nil {
					return err
				}
			}
			if converts {
				converted = true
			}
			return nil
		})
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if end, err := f.h.session.Closed(ctx); err != nil || end.Reason != "failed" {
			t.Fatal("conversion fault did not close", end, err)
		}
		store = f.crash()
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := mainRecoveryText(f.live(state)); ok {
			t.Error("converted partial remained stored", text)
		}
		f = mainOpenRecovery(t, store, ref, options, clock, math.Inf(1))
		mainFinishRecovery(t, f, streams, sub.ID(), []JSON{{"text": stored, "stopReason": "aborted"}, {"text": "answer", "stopReason": "stop"}})
	})
}

func TestMainGenerationAgentEventsBeforeStorage(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		ref, options, streams, clock := mainRecoverySetup(t)
		f := mainOpenRecovery(t, b.store, ref, options, clock, math.Inf(1))
		events := make(chan AgentEvent, 64)
		observer, err := f.h.WatchEvents(bg, f.c.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer observer.Stop()
		if err := observer.Start(func(_ context.Context, batch []AgentEvent) error {
			for _, event := range batch {
				events <- event
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		sub, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream := watchReceive(t, streams)
		for i, text := range []string{strings.Repeat("f", 64), strings.Repeat("f", 96)} {
			f.partial(stream, text)
			for {
				event := watchReceive(t, events)
				if (event.Type != "message_start" && event.Type != "message_update") || event.Task == 0 || event.Message == nil || event.Message.Role != goai.RoleAssistant {
					continue
				}
				want := "message_start"
				if i > 0 {
					want = "message_update"
				}
				if event.Type != want || event.Message == nil || event.Message.Content[0].Text != text {
					t.Fatal("memory agent event", event)
				}
				if i > 0 && len(event.Changes) == 0 {
					t.Error("partial update omitted changes")
				}
				break
			}
		}
		if text, ok := mainRecoveryText(f.live(f.stored())); ok {
			t.Fatal("agent events waited for storage", text)
		}
		var marker ID
		_, err = f.h.session.Commit(bg, func(tx *Tx) error {
			live, err := builtin(tx, f.c.ID(), "pi.live")
			if err != nil {
				return err
			}
			if _, err := live.Get(); err != nil {
				return err
			}
			marker, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: marker, Conversation: f.c.ID(), Kind: "test.marker", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		// The entry callback is an in-order barrier after the real dependent flush.
		for {
			event := watchReceive(t, events)
			if event.Type == "message_start" || event.Type == "message_update" {
				t.Fatal("storage replayed agent partial", event.Type)
			}
			if event.Type == "entry_appended" && event.Entry != nil && event.Entry.ID == marker {
				break
			}
		}
		if text, ok := mainRecoveryText(f.live(f.stored())); !ok || text != strings.Repeat("f", 96) {
			t.Fatal("dependent flush lost latest partial", text)
		}
		stream.events <- terminal("answer")
		stream.close()
		if got := waitSubmission(t, sub); got.Submission.Status != "done" {
			t.Fatal(got)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
