package durable

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestGeneration140PacingAlarmAloneResumesRetryAndPoll(t *testing.T) {
	for _, mode := range []string{"retry", "poll"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				api := goai.Api("pacing-140-" + strings.ReplaceAll(t.Name(), "/", "-"))
				var clock, streams, polls atomic.Int64
				clock.Store(1000)
				provider := &goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					events := make(chan goai.Event, 1)
					if streams.Add(1) > 1 {
						events <- terminal("retry answer")
					} else if mode == "retry" {
						events <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
					} else {
						events <- &goai.DoneEvent{Reason: goai.StopReasonDeferred, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonDeferred, Content: []goai.ContentBlock{}, Deferred: &goai.DeferredHandle{ID: "job", Api: string(api), Provider: string(goai.ProviderOpenAI), ModelID: "durable-test", PollAfterMs: 1000}}}
					}
					close(events)
					return events
				}, FetchDeferred: func(context.Context, *goai.Model, goai.DeferredHandle, *goai.StreamOptions) <-chan goai.Event {
					polls.Add(1)
					events := make(chan goai.Event, 1)
					events <- terminal("poll answer")
					close(events)
					return events
				}}
				goai.RegisterApi(provider)
				t.Cleanup(func() { goai.UnregisterApi(api) })
				armed := make(chan *capturedTimer140, 8)
				h := openHarness(t, b.store, Options{Now: clock.Load, Models: func(goai.Provider, string) *goai.Model { return fakeModel(api) }, SetTimer: func(callback func(), delay time.Duration, options TimerOptions) func() {
					if options.Background {
						return nil
					}
					if delay != time.Second {
						t.Errorf("sleep delay %v", delay)
					}
					node := &capturedTimer140{fire: callback}
					armed <- node
					return func() { node.cancelled.Add(1) }
				}})
				conversation, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}, Settings: RequestSettings{Retry: RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMs: 1000, MaxDelayMs: 1000}}})
				if err != nil {
					t.Fatal(err)
				}
				submitted, err := conversation.Submit(bg, Input{Content: "input", RequestID: "pacing"})
				if err != nil {
					t.Fatal(err)
				}
				next := func() *capturedTimer140 {
					t.Helper()
					select {
					case a := <-armed:
						return a
					case <-time.After(3 * time.Second):
						t.Fatal("builtin sleep alarm missing")
						return nil
					}
				}
				first := next()
				first.fire() // early: the same durable deadline must survive
				second := next()
				if streams.Load() != 1 || polls.Load() != 0 {
					t.Fatal("early alarm dispatched provider", streams.Load(), polls.Load())
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var taskID ID
				for _, task := range state.Tasks {
					if task.Kind == "pi.generation" {
						record, err := CanonicalTask(task, h.session.limits)
						if err != nil || record.State.Status != "sleeping" || record.State.Until == nil || *record.State.Until != 2000 {
							t.Fatal(record, err)
						}
						taskID = task.ID
					}
				}
				if taskID == 0 {
					t.Fatal("sleeping generation absent")
				}
				clock.Store(2000)
				second.fire()
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				// Observational polling only; a Wait/Resume would mask a lost alarm.
				for {
					state, err = h.Snapshot(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if terminalStatus(state.Submissions[submitted.ID()].Status) {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
				if state.Submissions[submitted.ID()].Status != "done" {
					t.Fatal(state.Submissions[submitted.ID()])
				}
				if mode == "retry" && (streams.Load() != 2 || polls.Load() != 0) || mode == "poll" && (streams.Load() != 1 || polls.Load() != 1) {
					t.Fatal(streams.Load(), polls.Load())
				}
			})
		})
	}
}
func TestGeneration140BuiltinSleepValidationAndProjection(t *testing.T) {
	checkpoint, err := dtoObject(generationCheckpoint{Phase: "retry", RetryUntil: 1000}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	original := Task{ID: 1, Conversation: 1, Kind: "pi.generation", Status: "pending", Checkpoint: checkpoint}
	if !sleepGeneration(&original) {
		t.Fatal("failed to attach builtin sleep metadata")
	}
	record, err := CanonicalTask(original, DefaultLimits())
	if err != nil || record.State.Until == nil || *record.State.Until != 1000 {
		t.Fatal(record, err)
	}
	*record.State.Until = 999
	if until, ok := taskSleepDeadline(original); !ok || until != 1000 {
		t.Fatal("projected deadline aliased checkpoint", until, ok)
	}
	for _, change := range []func(*Task){
		func(task *Task) { task.Execution = nil },
		func(task *Task) { task.Kind = "pi.tool" },
		func(task *Task) { task.Checkpoint["phase"] = "queued" },
		func(task *Task) { task.Checkpoint["phase"] = "poll"; delete(task.Checkpoint, "deferred") },
		func(task *Task) { task.Checkpoint["retryUntil"] = -1 },
		func(task *Task) { task.Checkpoint["retryUntil"] = int64(MaxID) + 1 },
		func(task *Task) { task.Checkpoint["retryUntil"] = json.Number("1000.5") },
	} {
		task, err := copyTask(original, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		change(&task)
		if _, err := copyTask(task, DefaultLimits()); err == nil {
			t.Fatal("invalid builtin sleep accepted", task)
		}
	}
}

func TestGeneration140LegacyPacingYieldsBeforeModelAccess(t *testing.T) {
	for _, phase := range []string{"retry", "poll"} {
		for _, status := range []string{"pending", "running"} {
			t.Run(phase+"-"+status, func(t *testing.T) {
				backends(t, func(t *testing.T, b backend) {
					rootID, taskID := mint(t, b.store), mint(t, b.store)
					cp := generationCheckpoint{Phase: phase, RetryUntil: 2000, PollAt: 2000}
					if phase == "poll" {
						cp.Deferred = &goai.DeferredHandle{ID: "legacy"}
					}
					checkpoint, err := dtoObject(cp, DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					apply(t, b.store, Write{Op: "create-conversation", Conversation: &Conversation{ID: rootID}}, Write{Op: "put-task", Task: &Task{ID: taskID, Conversation: rootID, Kind: "pi.generation", Status: status, Checkpoint: checkpoint}})
					var accesses atomic.Int64
					events := make(chan Wake, 64)
					h := openHarness(t, b.store, Options{Now: func() int64 { return 1000 }, OnWake: func(w Wake) { events <- copyWake(w) }, Models: func(goai.Provider, string) *goai.Model { accesses.Add(1); return nil }})
					if err = h.Resume(bg); err != nil {
						t.Fatal(err)
					}
					until := int64(2000)
					awaitWake140(t, events, false, &until)
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					task := state.Tasks[taskID]
					if task.Status != "sleeping" || task.Restarts != 0 || accesses.Load() != 0 {
						t.Fatal("legacy wait did not yield safely", task.Status, task.Restarts, accesses.Load())
					}
				})
			})
		}
	}
}
