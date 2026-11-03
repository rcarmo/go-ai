package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Official upstream v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5,
// harness-restart-failure.test.ts:275-328 (source-restart.ts source275).
// Native failed-Session/join/reopen replaces kill(); zero Restarts represents
// upstream's absent restarts. Both non-running states come from real work,
// never seeded task records or a healthy Close substituted for either crash.
func TestMainHarnessRetrySleepAndToolWaitDoNotChargeGenerationRestarts(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var clock, requests, executions atomic.Int64
		clock.Store(1000)
		gate := make(chan struct{})
		started := make(chan struct{}, 2)
		cancelled := make(chan struct{}, 2)
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{
			Definition:     goai.Tool{Name: "hold", Description: "Waits", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
			Implementation: "main.retry-tool-wait.hold",
			Version:        1,
			ReplaySafe:     true,
			Execute: func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
				executions.Add(1)
				started <- struct{}{}
				select {
				case <-gate:
					return ToolResult{Details: JSON{}}, nil
				case <-ctx.Done():
					cancelled <- struct{}{}
					return ToolResult{}, ctx.Err()
				}
			},
		}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event, 1)
			switch call := requests.Add(1); call {
			case 1:
				events <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{
					Role: goai.RoleAssistant, Content: []goai.ContentBlock{},
					StopReason: goai.StopReasonError, ErrorMessage: "503 Service Unavailable",
				}}
			case 2:
				events <- toolAnswer("c1", "hold", JSON{})
			case 3:
				events <- terminal("done")
			default:
				t.Errorf("unexpected provider request %d", call)
				events <- terminal("unexpected request")
			}
			close(events)
			return events
		})
		options.Registry = registry
		enabled, maximum, delay := true, 1, int64(60000)
		policy := &RetrySettings{Enabled: &enabled, MaxRetries: &maximum, BaseDelayMs: &delay}
		open := func(store Storage) *mainRecoveryHarness {
			f := mainOpenRecoveryWithRetry(t, store, ref, options, &clock, 0, policy)
			got := f.h.resolvedSettings(RequestSettings{}).Retry
			if !got.Enabled || got.MaxRetries != 1 || got.BaseDelayMs != 60000 {
				t.Fatalf("source275 own retry policy: got %+v; want enabled/max1/base60000ms", got)
			}
			return f
		}
		f := open(b.store)
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		submission, err := f.c.Submit(bg, Input{Content: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		task, _, ok, err := f.h.nextTask(f.c.ID())
		if err != nil || !ok {
			t.Fatal("original generation missing", task, err)
		}
		taskID := task.ID
		sleeping := observeTaskState(t, f.h, taskID, "sleeping")
		if sleeping.State.Checkpoint["phase"] != "retry" || sleeping.State.Until == nil || *sleeping.State.Until != 61000 || requests.Load() != 1 {
			t.Fatal("provider503 did not enter the actual 60000ms retry sleep", sleeping, requests.Load())
		}
		persisted := func() TaskRecord {
			state := f.stored()
			task, ok := state.Tasks[taskID]
			if !ok || task.Kind != "pi.generation" {
				t.Fatal("original generation absent from Storage", task)
			}
			record, err := CanonicalTask(task, f.h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			return record
		}
		if stored := persisted(); stored.State.Status != "sleeping" || stored.State.Until == nil || *stored.State.Until != 61000 {
			t.Fatal("retry sleep was not persisted before crash", stored)
		}
		store := f.crash()
		clock.Add(120000)
		f = open(store)
		// Source314: inspect BEFORE Resume; recovery must not charge a sleep.
		before, ok, err := f.h.Task(bg, taskID)
		if err != nil || !ok || before.Restarts != 0 {
			t.Fatal("source314: sleeping generation charged at reopen", before, err)
		}
		if before.State.Status != "sleeping" || before.State.Until == nil || *before.State.Until != 61000 || requests.Load() != 1 || executions.Load() != 0 {
			t.Fatal("first reopen changed the sleep or dispatched before Resume", before, requests.Load(), executions.Load())
		}
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		watchReceive(t, started)
		waiting, ok, err := f.h.Task(bg, taskID)
		// Source319: the canonical state (not the raw compatibility status)
		// must be waiting for its real gated tool round, checkpoint phase tools.
		if err != nil || !ok || waiting.State.Status != "waiting" || waiting.State.Checkpoint["phase"] != "tools" {
			t.Fatal("source319: generation not canonically waiting for tools", waiting, err)
		}
		if stored := persisted(); stored.State.Status != "waiting" || stored.State.Checkpoint["phase"] != "tools" || len(stored.State.On) != 1 || requests.Load() != 2 || executions.Load() != 1 {
			t.Fatal("gated tool wait was not persisted before second crash", stored, requests.Load(), executions.Load())
		}
		store = f.crash()
		watchReceive(t, cancelled) // the held invocation actually stopped/joined
		f = open(store)
		// Source323: again inspect BEFORE Resume and before releasing the gate.
		before, ok, err = f.h.Task(bg, taskID)
		if err != nil || !ok || before.Restarts != 0 {
			t.Fatal("source323: tool-waiting generation charged at reopen", before, err)
		}
		if before.State.Status != "waiting" || before.State.Checkpoint["phase"] != "tools" || requests.Load() != 2 || executions.Load() != 1 {
			t.Fatal("second reopen changed the tool wait or dispatched before Resume", before, requests.Load(), executions.Load())
		}
		close(gate)
		if err := f.h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		recovered, err := f.h.Submission(bg, submission.ID())
		if err != nil {
			t.Fatal(err)
		}
		// Source326: settle the ORIGINAL submission, not a replacement input.
		settled := waitSubmission(t, recovered)
		if settled.Submission.Status != "done" {
			t.Fatal("source326: original submission did not finish", settled)
		}
		if requests.Load() != 3 || executions.Load() != 2 || settled.Message == nil || len(settled.Message.Content) != 1 || settled.Message.Content[0].Text != "done" {
			t.Fatal("safe hold replay or final provider response missing", requests.Load(), executions.Load(), settled)
		}
		if err := f.h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
