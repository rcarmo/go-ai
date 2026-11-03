package durable

import (
	"context"
	"testing"
	"time"
)

// Actual f9a26365 harness-after-run.test.ts882: a finish-phase crash while the
// hook is blocked, then extension deselection; no re-ask or child invocation.
func TestMainAfterRunHooksDeselectedBeforeRecoveryFinish(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		p := mainAfterRecoverySetup(t, map[int64]chan struct{}{1: make(chan struct{})}, nil, nil)
		h, c, _ := mainAfterRecoveryStart(t, p, b.store)
		watchReceive(t, p.hookEntered)
		store := mainAfterRecoveryCrash(t, h, b.store)
		p.options.Registry.Uninstall(&Extension{Name: "after-recovery"})
		h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		mainAfterRecoveryIdle(t, c)
		if live := mainAfterRunLive(t, h, c.ID()); len(live) != 0 {
			t.Error("deselected hook left live state", live)
		}
		if p.asks.Load() != 1 || p.tasks.Load() != 0 || p.calls.Load() != 1 {
			t.Error("deselected hook re-asked/dispatched", p.asks.Load(), p.tasks.Load(), p.calls.Load())
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

// Actual f9a26365 harness-after-run.test.ts903: forking at the committed answer
// while after-run is gated must not copy the active run or queued inbox.
func TestMainAfterRunForkStartsIdleWithoutParentQueue(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		gate := make(chan struct{})
		p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: gate}, nil)
		h, c, settled := mainAfterRecoveryStart(t, p, b.store)
		cleanupTaskGates(t, gate)
		watchReceive(t, p.taskEntered)
		later, err := c.Submit(bg, Input{Content: "later"})
		if err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		queued := false
		for _, doc := range state.Documents {
			if !doc.Retired && doc.Owner == c.ID() && doc.Kind == "pi.inbox" {
				items, _ := doc.Value["items"].([]any)
				queued = len(items) == 1 && submissionEntryID(generationObject(items[0])["id"]) == later.ID()
			}
		}
		if !queued {
			t.Fatal("parent input was not queued behind after-run")
		}
		fork, err := c.Fork(bg, settled.Record.Answer)
		if err != nil {
			t.Fatal(err)
		}
		live := mainAfterRunLive(t, h, fork.ID())
		if live["run"] != nil {
			t.Error("fork copied active parent run", live)
		}
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		count := -1
		for _, doc := range state.Documents {
			if !doc.Retired && doc.Owner == fork.ID() && doc.Kind == "pi.inbox" {
				items, _ := doc.Value["items"].([]any)
				count = len(items)
			}
		}
		if count != 0 {
			t.Error("fork copied parent queue or lacks inbox", count)
		}
		owner := state.Conversations[fork.ID()].Owner
		if owner != 0 {
			t.Error("fork is not ownerless", owner)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := fork.WaitForIdle(ctx); err != nil {
			t.Fatal("fork did not start idle", err)
		}
		// The fork must not consume or release the original host's work.
		live = mainAfterRunLive(t, h, c.ID())
		if live["run"] == nil || p.tasks.Load() != 1 || p.calls.Load() != 1 {
			t.Fatal("fork changed the parent run", live, p.tasks.Load(), p.calls.Load())
		}
		releaseTaskGate(gate)
		mainAfterRecoveryIdle(t, c)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
