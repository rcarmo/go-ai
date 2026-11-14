package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompactionLiveStatusRetrySnapshotReopenAndRemoval(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var now atomic.Int64
		now.Store(42)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
			} else {
				ch <- terminal("summary")
			}
			close(ch)
			return ch
		})
		options.Now = now.Load
		wakes := make(chan Wake, 128)
		options.OnWake = func(w Wake) { wakes <- copyWake(w) }
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		zero := 0
		configureCompactionTestPolicy(t, conversation, &zero, nil)
		events, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer events.Stop()
		started := make(chan AgentEvent, 1)
		if err := events.Start(func(_ context.Context, batch []AgentEvent) error {
			for _, event := range batch {
				if event.Type == "compaction_start" {
					started <- event
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		id, err := conversation.Compact(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		state := waitCompactionCheckpoint(t, h, id, "retry")
		select {
		case event := <-started:
			if event.Task != id || event.Reason != "manual" || event.Blocking {
				t.Error("derived event lost canonical trigger", event)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("missing committed compaction event")
		}
		events.Stop()
		record, err := CanonicalTask(state.Tasks[id], h.session.limits)
		if err != nil || record.State.Status != "sleeping" || record.State.Until == nil || *record.State.Until != 2042 {
			t.Fatal("compaction retry did not durably sleep", record.State, err)
		}
		checkpoint := taskCheckpointMap(record.State.Checkpoint)
		if !equalJSONValue(checkpoint["attempt"], 1) {
			t.Error("retry must retain the failed attempt until the next request", checkpoint)
		}
		if _, private := checkpoint["retryError"]; private {
			t.Error("retry error belongs to LiveDoc status, not checkpoint", checkpoint)
		}
		until := int64(2042)
		awaitWake140(t, wakes, false, &until)
		var status map[string]any
		for _, document := range state.Documents {
			if document.Kind == "pi.live" && document.Owner == conversation.ID() {
				items := document.Value["compactions"].([]any)
				if len(items) != 1 {
					t.Fatal(items)
				}
				status = items[0].(map[string]any)
				if document.Version != 3 {
					t.Error("source LiveDoc must be version3", document.Version)
				}
			}
		}
		// Source live.ts:70-81: compare the complete stored v3 status, not a
		// migrated copy that could hide a writer still emitting version1 keys.
		data, err := os.ReadFile("testdata/pi-durable-v150/compaction-retry.json")
		if err != nil {
			t.Fatal(err)
		}
		var original struct {
			Live JSON `json:"live"`
		}
		if err := json.Unmarshal(data, &original); err != nil {
			t.Fatal(err)
		}
		want := original.Live["compactions"].([]any)[0]
		if !equalJSONValue(status, want) {
			t.Error("noncanonical stored compaction status", status)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		stream, err := h.WatchEvents(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := stream.Snapshot()
		if err != nil || len(snapshot.Documents["pi.live"]["compactions"].([]any)) != 1 {
			t.Fatal(snapshot, err)
		}
		if !equalJSONValue(snapshot.Documents["pi.live"]["compactions"].([]any)[0], want) {
			t.Error("noncanonical status after SQLite reopen", snapshot.Documents["pi.live"])
		}
		now.Store(2042)
		waitPublicTask(t, h, id)
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, document := range state.Documents {
			if document.Kind == "pi.live" && document.Owner == conversation.ID() {
				if _, exists := document.Value["compactions"]; exists {
					t.Fatal("terminal compaction status retained", document)
				}
			}
		}
		stream.Stop()
	})
}

func waitCompactionCheckpoint(t *testing.T, h *Harness, id ID, phase string) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	state, subscription, err := h.SubscribeCommits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Stop()
	matches := func(task Task) bool {
		record, err := CanonicalTask(task, h.session.limits)
		return err == nil && taskCheckpointMap(record.State.Checkpoint)["phase"] == phase
	}
	if matches(state.Tasks[id]) {
		return state
	}
	reached := make(chan struct{}, 1)
	signal := func() {
		select {
		case reached <- struct{}{}:
		default:
		}
	}
	if err := subscription.Start(func(_ context.Context, frame PublicationFrame) error {
		if frame.Snapshot != nil && matches(frame.Snapshot.Tasks[id]) {
			signal()
		}
		for _, write := range frame.Publication.Tables {
			if write.Task != nil && write.Task.ID == id && matches(*write.Task) {
				signal()
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, reached)
	state, err = h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !matches(state.Tasks[id]) {
		t.Fatal("compaction phase changed before capture", phase)
	}
	return state
}

func TestCompaction120FailureReopenNoCutAndSummaryClearing(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("summary")
			close(ch)
			return ch
		})
		resolve := options.Models
		options.Models = func(goai.Provider, string) *goai.Model { return nil }
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		failed, err := conv.Compact(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, h, failed); record.State.Outcome.Status != "failed" {
			t.Fatal(record)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		options.Models = resolve
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		conv, err = next.Conversation(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		failure := func() *CompactionFailure {
			state, err := next.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			view, err := conversationSnapshot(state, conv.ID(), next.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			return view.CompactionFailure
		}
		if f := failure(); f == nil || f.Task != failed {
			t.Fatal("failure lost on reopen", f)
		}
		noCut, err := conv.Compact(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, next, noCut); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		} else if record.State.Outcome.Result == nil || !equalJSONValue(record.State.Outcome.Result.Value, JSON{}) {
			// Source compaction.ts complete() returns a present empty result.
			t.Error("no-cut result must be exactly {}", record.State.Outcome.Result)
		}
		if f := failure(); f == nil || f.Task != failed {
			t.Fatal("no-cut cleared failure", f)
		}
		_, err = conv.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old question", "old answer", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: conv.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		configureCompactionTestPolicy(t, conv, compactionTestInt(1), nil)
		summary, err := conv.Compact(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		if record := waitPublicTask(t, next, summary); record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		if f := failure(); f != nil {
			t.Fatal("summary did not clear failure", f)
		}
	})
}
