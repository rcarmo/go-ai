package durable

import (
	"context"
	"math"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Official f9a26365 harness-deferred-progress.test.ts:210-226. The provider is
// hand-stepped rather than wall-clock streamed; the last observed memory value
// is quiescent before Close. Storage is reopened only after the host joins.
func TestMainHarnessCleanCloseStoresHeldGeneration(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ref, options, streams, clock := mainRecoverySetup(t)
		f := mainOpenSlowRecovery(t, b.store, ref, options, clock, math.Inf(1))
		sub, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		stream := watchReceive(t, streams)
		f.partial(stream, strings.Repeat("close ", 16))
		atClose := f.memory[len(f.memory)-1]
		if len(atClose) <= 40 {
			t.Fatal("missing memory partial", atClose)
		}
		if text, ok := mainRecoveryText(f.live(f.stored())); ok {
			t.Fatal("partial was not held", text)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := utf16ReopenStore(t, f.store)
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := mainRecoveryText(f.live(state)); !ok || got != atClose {
			t.Errorf("close stored=%q want=%q", got, atClose)
		}
		// Existing generic clean-close coverage checks this budget invariant; this
		// control binds it to the production generation with held presentation.
		generations := 0
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" {
				generations++
				if task.Status != "pending" || task.Restarts != 0 {
					t.Error("clean close charged interruption", task)
				}
			}
		}
		if generations != 1 {
			t.Fatal("generation count", generations)
		}
		f = mainOpenSlowRecovery(t, store, ref, options, clock, math.Inf(1))
		state, err = f.h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" && task.Restarts != 0 {
				t.Error("reopen charged clean close", task)
			}
		}
		mainFinishRecovery(t, f, streams, sub.ID(), []JSON{{"text": atClose, "stopReason": "aborted"}, {"text": "answer", "stopReason": "stop"}})
	})
}

// Source228-248 preserves the stored deferred handle through a crash, resumes
// at 61000ms and fetches exactly once. The old native deferred_test.go checks
// a healthy close instead. Additional request/attempt assertions are controls,
// not extra upstream assertions. No HTTP, credentials or real timers are used.
func TestMainHarnessDeferredPollAfterCrash(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		api := goai.Api("main-crash-poll-" + strings.ReplaceAll(t.Name(), "/", "-"))
		var requests, polls atomic.Int64
		clock := new(atomic.Int64)
		clock.Store(1000)
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, _ *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
			requests.Add(1)
			if opts.Deferred == nil {
				t.Error("deferred request option lost")
			}
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonDeferred, Deferred: &goai.DeferredHandle{ID: "job", PollAfterMs: 60000}}}
			close(ch)
			return ch
		}, FetchDeferred: func(_ context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) <-chan goai.Event {
			polls.Add(1)
			if handle.ID != "job" || clock.Load() != 61000 {
				t.Error("wrong deferred fetch", handle, clock.Load())
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("deferred answer")
			close(ch)
			return ch
		}})
		t.Cleanup(func() { goai.UnregisterApi(api) })
		ref := ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}
		options := Options{Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }}
		f := mainOpenRecovery(t, b.store, ref, options, clock, math.Inf(1))
		if err := f.c.Configure(bg, AgentChange{Model: ref, Settings: RequestSettings{Deferred: &goai.DeferredOptions{PollAfterMs: 60000}}}); err != nil {
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
		state := f.stored()
		generation, _ := f.live(state)["generation"].(map[string]any)
		if generation["deferred"] == nil {
			t.Fatal("deferred handle not stored", generation)
		}
		if requests.Load() != 1 || polls.Load() != 0 {
			t.Fatal("early fetch", requests.Load(), polls.Load())
		}
		store := f.crash()
		f = mainOpenRecovery(t, store, ref, options, clock, math.Inf(1))
		clock.Store(61000)
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		resumed, err := f.h.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, resumed)
		if result.Submission.Status != "done" || result.Message == nil || result.Message.Content[0].Text != "deferred answer" {
			t.Fatal("deferred recovery", result)
		}
		// Canonical input maps to the legacy raw follow-up representation.
		settled, err := resumed.Status(bg)
		if err != nil || settled.Type != "input" || settled.Status != "done" || settled.Answer == 0 {
			t.Fatal("deferred input settlement", settled, err)
		}
		if requests.Load() != 1 || polls.Load() != 1 {
			t.Error("request replayed or wrong fetch count", requests.Load(), polls.Load())
		}
		var cp generationCheckpoint
		if err := fromObject(result.Task.Checkpoint, &cp, f.h.session.limits); err != nil {
			t.Fatal(err)
		}
		if cp.Attempt != 1 {
			t.Error("poll charged request attempt", cp.Attempt)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
