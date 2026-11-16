package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Public stored publications are the barrier; no private checkpoint or polling.
// Source harness-generation.test.ts:287-374 and deferred-progress:228-248.
type generationPublications struct {
	tasks chan TaskRecord
	live  chan JSON
}

func recordGenerationPublications(t *testing.T, h *Harness) *generationPublications {
	t.Helper()
	out := &generationPublications{make(chan TaskRecord, 64), make(chan JSON, 64)}
	guard, err := h.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
		for i := 0; i < p.Changes.Len(); i++ {
			change, _ := p.Changes.Index(i)
			kind, _ := change.Get("type")
			value, _ := change.Get("value")
			switch kind.String() {
			case "task":
				name, _ := value.Get("kind")
				if name.String() != "pi.generation" {
					continue
				}
				raw, err := json.Marshal(value.Copy())
				if err != nil {
					return err
				}
				var task TaskRecord
				if err = json.Unmarshal(raw, &task); err != nil {
					return err
				}
				select {
				case out.tasks <- task:
				default:
					return fmt.Errorf("generation publication test capacity exceeded")
				}
			case "document":
				record, _ := change.Get("record")
				name, _ := record.Get("kind")
				if name.String() != "pi.live" {
					continue
				}
				select {
				case out.live <- JSON(value.Copy().(map[string]any)):
				default:
					return fmt.Errorf("live publication test capacity exceeded")
				}
			}
		}
		return nil
	}, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(guard.Detach)
	return out
}
func (p *generationPublications) waitTask(t *testing.T, phase, status string) TaskRecord {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		select {
		case task := <-p.tasks:
			if task.State.Status == status && taskCheckpointMap(task.State.Checkpoint)["phase"] == phase {
				return task
			}
		case <-ctx.Done():
			t.Fatalf("no stored generation %s/%s", phase, status)
		}
	}
}

func (p *generationPublications) waitLive(t *testing.T, matches func(JSON) bool) JSON {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		select {
		case live := <-p.live:
			if matches(live) {
				return live
			}
		case <-ctx.Done():
			t.Fatal("no matching stored live publication")
		}
	}
}

func TestDeferredGenerationPollCheckpointReopenNoRedispatch(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		api := goai.Api("durable-deferred-" + strings.ReplaceAll(t.Name(), "/", "-"))
		var streams, polls atomic.Int64
		var clock atomic.Int64
		clock.Store(1000)
		provider := &goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, _ *goai.Context, options *goai.StreamOptions) <-chan goai.Event {
			streams.Add(1)
			if options.Deferred == nil {
				t.Error("deferred option not pinned")
			}
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonDeferred, Deferred: &goai.DeferredHandle{ID: "job", Provider: string(goai.ProviderOpenAI), ModelID: "durable-test", Api: string(api), PollAfterMs: 1000, Data: JSON{"safe": "handle"}}}}
			close(ch)
			return ch
		}, FetchDeferred: func(_ context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) <-chan goai.Event {
			polls.Add(1)
			if handle.ID != "job" {
				t.Error("handle changed", handle)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("polled answer")
			close(ch)
			return ch
		}}
		goai.RegisterApi(provider)
		t.Cleanup(func() { goai.UnregisterApi(api) })
		wakes := make(chan Wake, 128)
		options := Options{Settings: &HarnessSettings{Stream: RequestSettings{Deferred: &goai.DeferredOptions{PollAfterMs: 1000}}}, Now: clock.Load, OnWake: func(w Wake) { wakes <- copyWake(w) }, Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }}
		first := openHarness(t, b.store, options)
		conversation, err := first.Root(bg, AgentChange{Model: ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}})
		if err != nil {
			t.Fatal(err)
		}
		publications := recordGenerationPublications(t, first)
		sub, err := conversation.Submit(bg, Input{Content: "defer"})
		if err != nil {
			t.Fatal(err)
		}
		record := publications.waitTask(t, "poll", "sleeping")
		cp := taskCheckpointMap(record.State.Checkpoint)
		if submissionEntryID(cp["pollAt"]) != 2000 || generationObject(cp["handle"])["id"] != "job" || record.State.Until == nil || *record.State.Until != 2000 {
			t.Fatal("stored poll checkpoint", record)
		}
		until := int64(2000)
		awaitWake140(t, wakes, false, &until)
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		if streams.Load() != 1 || polls.Load() != 0 {
			t.Fatal("poll occurred before deadline", streams.Load(), polls.Load())
		}
		clock.Store(2000)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := second.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || result.Message.Content[0].Text != "polled answer" || streams.Load() != 1 || polls.Load() != 1 {
			t.Fatal("deferred replay", result, streams.Load(), polls.Load())
		}
	})
}

func TestDeferredRepollKeepsRequestAttemptAndSkipsRequestHooks(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		api := goai.Api("durable-repoll-" + strings.ReplaceAll(t.Name(), "/", "-"))
		var streams, polls, before, after atomic.Int64
		var clock atomic.Int64
		clock.Store(1000)
		handle := goai.DeferredHandle{ID: "repeat", PollAfterMs: 1}
		pending := func() <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonDeferred, Deferred: &handle}}
			close(ch)
			return ch
		}
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			streams.Add(1)
			return pending()
		}, FetchDeferred: func(context.Context, *goai.Model, goai.DeferredHandle, *goai.StreamOptions) <-chan goai.Event {
			if polls.Add(1) == 1 {
				return pending()
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("ready")
			close(ch)
			return ch
		}})
		t.Cleanup(func() { goai.UnregisterApi(api) })
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "poll-hooks", Hooks: GenerationHooks{BeforeRequest: func(context.Context, []MessageReceipt, *HookAPI) ([]MessageReceipt, error) {
			before.Add(1)
			return nil, nil
		}, AfterResponse: func(context.Context, MessageReceipt, *HookAPI) error { after.Add(1); return nil }}}); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Now: func() int64 { return clock.Add(10) }, Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }})
		conversation := root(t, h, ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"})
		publications := recordGenerationPublications(t, h)
		sub, err := conversation.Submit(bg, Input{Content: "deferred"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		attempts := 0
		for len(publications.tasks) > 0 {
			record := <-publications.tasks
			cp := taskCheckpointMap(record.State.Checkpoint)
			if cp["phase"] == "poll" {
				attempts++
				if submissionEntryID(cp["attempt"]) != 1 {
					t.Fatal("poll attempt changed", record)
				}
			}
		}
		times := []ID{}
		for len(publications.live) > 0 {
			live := <-publications.live
			if deferred := generationObject(generationObject(live["generation"])["deferred"]); deferred != nil {
				times = append(times, submissionEntryID(deferred["pollAt"]))
			}
		}
		if attempts == 0 || len(times) != 2 || times[1] <= times[0] {
			t.Fatal("poll publications", attempts, times)
		}
		if result.Record.Status != "done" || result.Record.Type != "input" || result.Message == nil || result.Message.Content[0].Text != "ready" || streams.Load() != 1 || polls.Load() != 2 || before.Load() != 1 || after.Load() != 1 {
			t.Fatal("poll became a new request", result, streams.Load(), polls.Load(), before.Load(), after.Load())
		}

	})
}
func TestDeferredExpiryOnReopenStillPollsProvider(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		api := goai.Api("durable-expiry-" + strings.ReplaceAll(t.Name(), "/", "-"))
		var streams, polls atomic.Int64
		var clock atomic.Int64
		clock.Store(1000)
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			streams.Add(1)
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonDeferred, Deferred: &goai.DeferredHandle{ID: "expires", ExpiresAt: 1500, PollAfterMs: 1000}}}
			close(ch)
			return ch
		}, FetchDeferred: func(context.Context, *goai.Model, goai.DeferredHandle, *goai.StreamOptions) <-chan goai.Event {
			polls.Add(1)
			ch := make(chan goai.Event, 1)
			ch <- terminal("provider decides expired handle")
			close(ch)
			return ch
		}})
		t.Cleanup(func() { goai.UnregisterApi(api) })
		options := Options{Now: clock.Load, Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }}
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"})
		publications := recordGenerationPublications(t, first)
		sub, err := conversation.Submit(bg, Input{Content: "expire"})
		if err != nil {
			t.Fatal(err)
		}
		record := publications.waitTask(t, "poll", "sleeping")
		if submissionEntryID(generationObject(taskCheckpointMap(record.State.Checkpoint)["handle"])["expiresAt"]) != 1500 {
			t.Fatal("expiry checkpoint", record)
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		clock.Store(2000)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		handle, err := second.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" || result.Message.Content[0].Text != "provider decides expired handle" || streams.Load() != 1 || polls.Load() != 1 {
			t.Fatal("expired handle dispatched", result, streams.Load(), polls.Load())
		}
	})
}
