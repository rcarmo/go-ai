package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Actual f9a26365 harness-after-run.test.ts785: reopen without the extension
// while a real after-run child is running. It remains blocked/busy until Stop
// orphans it, clearing the already answered run without runFailure.
func TestMainAfterRunMissingDefinitionStopOrphans(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: make(chan struct{})}, nil)
		h, c, settled := mainAfterRecoveryStart(t, p, b.store)
		watchReceive(t, p.taskEntered)
		live := mainAfterRunLive(t, h, c.ID())
		run := generationObject(live["run"])
		after := generationObject(run["afterRun"])
		tasks, ok := after["tasks"].([]any)
		if !ok || len(tasks) != 1 {
			t.Fatal("real after-run child missing", live)
		}
		childID := submissionEntryID(tasks[0])
		parentID := submissionEntryID(run["task"])
		if parentID != settled.Task.ID {
			t.Fatal("wrong generation owner", parentID, settled)
		}
		child, ok, err := h.Task(bg, childID)
		if err != nil || !ok || child.Owner != parentID || child.State.Status != "running" {
			t.Fatal("real child must be running before crash", child, ok, err)
		}
		store := mainAfterRecoveryCrash(t, h, b.store)
		bare := p.options
		bare.Registry = NewRegistry()
		h, c = mainSalvageOpen(t, store, bare, c.ID())
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		live = mainAfterRunLive(t, h, c.ID())
		run = generationObject(live["run"])
		after = generationObject(run["afterRun"])
		tasks, ok = after["tasks"].([]any)
		if !ok || len(tasks) != 1 || submissionEntryID(tasks[0]) != childID || submissionEntryID(run["task"]) != parentID {
			t.Fatal("lost after-run ownership on reopen", live)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			view, err := h.Inspect(ctx)
			if err != nil {
				t.Fatal(err)
			}
			blocked := false
			for _, item := range view.Tasks {
				if item.Record.ID == childID && item.Kind == "blocked" {
					blocked = true
				}
			}
			if blocked {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("missing-definition child never inspected blocked", view)
			case <-time.After(time.Millisecond):
			}
		}
		// A bounded caller cancellation witnesses that public WaitForIdle remains
		// unsettled; it must not clear or fault the blocked run.
		waiting, stopWaiting := context.WithTimeout(ctx, 25*time.Millisecond)
		err = c.WaitForIdle(waiting)
		stopWaiting()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("blocked after-run became idle", err)
		}
		live = mainAfterRunLive(t, h, c.ID())
		if live["run"] == nil || live["runFailure"] != nil {
			t.Fatal("blocked run changed before Stop", live)
		}
		if err := c.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal("Stop did not release the answered run", err)
		}
		live = mainAfterRunLive(t, h, c.ID())
		if live["run"] != nil || live["runFailure"] != nil || live["failedRun"] != nil {
			t.Error("answered Stop retained run/failure", live)
		}
		child, ok, err = h.Task(ctx, childID)
		if err != nil || !ok || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "orphaned" {
			t.Error("missing-definition child not orphaned", child, ok, err)
		}
		mainAfterRecoveryOutcome(t, h, parentID)
		status := mainCutoffStatus(t, h, settled.Record.ID)
		if status.Status != "done" || status.Answer != settled.Record.Answer {
			t.Error("Stop unsettled committed answer", status, settled)
		}
		if p.asks.Load() != 1 || p.calls.Load() != 1 || p.tasks.Load() != 1 {
			t.Error("reopened host redispatched", p.asks.Load(), p.calls.Load(), p.tasks.Load())
		}
		if err := h.Close(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
