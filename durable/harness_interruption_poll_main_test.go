package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source f9a26365 harness-restart-failure.test.ts:523-594. Native scripted
// polling uses 10ms in all modes (literal source574; pacing adapter for523/554).
func TestMainInterruptedPollRetryEdges(t *testing.T) {
	for _, mode := range []string{"retry", "disabled", "backwards"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				api := goai.Api("main-interrupted-poll-" + strings.ReplaceAll(t.Name(), "/", "-"))
				var requests, polls, cancels atomic.Int64
				clock := new(atomic.Int64)
				clock.Store(1000)
				fetching := make(chan struct{})
				goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					requests.Add(1)
					events := make(chan goai.Event, 1)
					events <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonDeferred, Deferred: &goai.DeferredHandle{ID: "job", PollAfterMs: 10}}}
					close(events)
					return events
				}, FetchDeferred: func(ctx context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) <-chan goai.Event {
					if handle.ID != "job" {
						t.Error("fixture deferred handle changed", handle)
					}
					if polls.Add(1) == 1 {
						stream := &mainRecoveryStream{events: make(chan goai.Event)}
						close(fetching)
						go func() { <-ctx.Done(); stream.close() }()
						return stream.events
					}
					events := make(chan goai.Event, 1)
					events <- terminal("deferred answer")
					close(events)
					return events
				}, CancelDeferred: func(_ context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) error {
					if handle.ID != "job" {
						t.Error("fixture cancelled a different job", handle)
					}
					cancels.Add(1)
					return nil
				}})
				t.Cleanup(func() { goai.UnregisterApi(api) })
				ref := ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}
				options := Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }}
				enabled, maximum, delay := mode == "retry", 2, int64(1)
				policy := &RetrySettings{Enabled: &enabled, MaxRetries: &maximum, BaseDelayMs: &delay}
				f := mainOpenRecoveryWithRetry(t, b.store, ref, options, clock, 0, policy)
				if err := f.c.Configure(bg, AgentChange{Model: ref, Settings: RequestSettings{Deferred: &goai.DeferredOptions{PollAfterMs: 10}}}); err != nil {
					t.Fatal(err)
				}
				sub, err := f.c.Submit(bg, Input{Content: "hi"})
				if err != nil {
					t.Fatal(err)
				}
				for {
					frame := watchReceive(t, f.frames)
					generation, _ := frame.value.Docs["pi.live"]["generation"].(map[string]any)
					if generation["deferred"] != nil {
						break
					}
				}
				clock.Store(1010)
				if err := f.h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				watchReceive(t, fetching)
				task, cp, ok, err := f.h.nextTask(f.c.ID())
				if err != nil || !ok || cp.Phase != "poll" || task.Status != "running" || cp.PollAt != 1010 {
					t.Fatal("fixture did not crash during the poll fetch", task, cp, err)
				}
				store := f.crash()
				if mode == "backwards" {
					clock.Store(1000)
				}
				f = mainOpenRecoveryWithRetry(t, store, ref, options, clock, 0, policy)
				before, exists, err := f.h.Task(bg, task.ID)
				if err != nil || !exists || before.Restarts != 1 {
					t.Fatal("fixture lost its poll crash marker", before, err)
				}
				if err := f.h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := mainInterruptionQuiescent(ctx, f.h); err != nil {
					t.Fatal("fixture poll recovery did not settle admitted work", err)
				}
				if mode == "retry" {
					record, exists, err := f.h.Task(bg, task.ID)
					if err != nil || !exists {
						t.Fatal(err)
					}
					if record.State.Status != "sleeping" || record.State.Until == nil || *record.State.Until != 1011 || record.State.Checkpoint["phase"] != "retry" {
						t.Errorf("interrupted poll must back off before fetching again: %+v", record)
					}
					state, err := f.h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					want := JSON{"attempt": 1, "retry": JSON{"at": 1011, "error": "The host stopped while the deferred answer was fetched, on attempt 1"}}
					if got := f.live(state)["generation"]; !equalJSONValue(got, want) {
						t.Errorf("interrupted poll retry projection: got %v; want %v", got, want)
					}
					if polls.Load() != 1 {
						t.Errorf("interrupted poll fetched before retry deadline: %d", polls.Load())
					}
					clock.Store(1011)
					if err := f.h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					if err := mainInterruptionQuiescent(ctx, f.h); err != nil {
						t.Fatal(err)
					}
				}
				handle, err := f.h.Submission(bg, sub.ID())
				if err != nil {
					t.Fatal(err)
				}
				status, err := handle.Status(bg)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "retry" {
					if status.Status != "done" || requests.Load() != 1 || polls.Load() != 2 || cancels.Load() != 0 {
						t.Errorf("poll retry must reuse its job: %+v requests=%d polls=%d cancels=%d", status, requests.Load(), polls.Load(), cancels.Load())
					}
					var sawSecond bool
					if status.Answer == 0 {
						t.Fatal("poll retry did not produce an answer")
					}
					for _, frame := range awaitUpstreamWindow(t, f.frames, status.Answer) {
						generation, _ := frame.value.Docs["pi.live"]["generation"].(map[string]any)
						if equalJSONValue(generation["attempt"], 2) && generation["deferred"] != nil {
							sawSecond = true
						}
					}
					if !sawSecond {
						t.Error("poll retry never exposed attempt 2 with its deferred handle")
					}
					state, err := f.h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					answer := state.Entries[status.Answer].Value
					var message MessageReceipt
					if err := fromObject(answer, &message, f.h.session.limits); err != nil || len(message.Content) != 1 || message.Content[0].Text != "deferred answer" {
						t.Errorf("poll retry answer: %+v, %v", message, err)
					}
				} else {
					text := "The host stopped while the deferred answer was fetched, on attempt 1"
					if status.Status != "unanswered" || status.Reason != "interrupted" || status.Detail == nil || !status.Detail.Present || status.Detail.Value != text {
						t.Errorf("disabled interrupted poll must settle despite its old deadline: %+v", status)
					}
					if requests.Load() != 1 || polls.Load() != 1 || cancels.Load() != 1 {
						t.Errorf("disabled interrupted poll must cancel once without fetching again: requests=%d polls=%d cancels=%d", requests.Load(), polls.Load(), cancels.Load())
					}
					if mode == "disabled" {
						state, err := f.h.Snapshot(bg)
						if err != nil {
							t.Fatal(err)
						}
						mainRequireRunFailure(t, state, f.c.ID(), task.ID, sub.ID(), status.Entry, "interrupted", text, true)
					}
				}
			})
		})
	}
}
