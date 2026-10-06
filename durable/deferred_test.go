package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeferredGenerationPollCheckpointReopenNoRedispatch(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
		options := Options{Now: clock.Load, Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }}
		first := openHarness(t, b.store, options)
		conversation, err := first.Root(bg, AgentChange{Model: ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}, Settings: RequestSettings{Deferred: &goai.DeferredOptions{PollAfterMs: 1000}}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "defer"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		found := false
		for !found {
			state, err := first.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range state.Tasks {
				if task.Kind == "pi.generation" {
					var cp generationCheckpoint
					if err := fromObject(task.Checkpoint, &cp, first.session.limits); err != nil {
						t.Fatal(err)
					}
					if cp.Phase == "poll" {
						found = true
						if cp.PollAt != 2000 || cp.Deferred.ID != "job" {
							t.Fatal(cp)
						}
					}
				}
			}
		}
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
	backends(t, func(t *testing.T, b backend) {
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
		sub, err := conversation.Submit(bg, Input{Content: "deferred"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		var cp generationCheckpoint
		if err := fromObject(result.Task.Checkpoint, &cp, h.session.limits); err != nil {
			t.Fatal(err)
		}
		if result.Submission.Status != "done" || cp.Attempt != 1 || streams.Load() != 1 || polls.Load() != 2 || before.Load() != 1 || after.Load() != 1 {
			t.Fatal("poll became a new request", result, cp.Attempt, streams.Load(), polls.Load(), before.Load(), after.Load())
		}
	})
}
func TestDeferredExpiryOnReopenStillPollsProvider(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
		sub, err := conversation.Submit(bg, Input{Content: "expire"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			state, err := first.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, task := range state.Tasks {
				if task.Kind == "pi.generation" {
					var cp generationCheckpoint
					if err := fromObject(task.Checkpoint, &cp, first.session.limits); err != nil {
						t.Fatal(err)
					}
					found = cp.Phase == "poll"
				}
			}
			if found {
				break
			}
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
