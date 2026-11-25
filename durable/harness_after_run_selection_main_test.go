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

// harness-after-run.test.ts:904-921. A fork at the committed answer must
// not copy the held after-run state or the queued input from its parent.
func TestMainAfterRunForkStartsIdleWithoutParentQueue(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ctx, cancel := context.WithTimeout(bg, 8*time.Second)
		defer cancel()
		gate := make(chan struct{})
		p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: gate}, nil)
		h, c, settled := mainAfterRecoveryStart(t, p, b.store)
		cleanupTaskGates(t, gate)
		watchReceive(t, p.taskEntered)
		later, err := c.Submit(ctx, Input{Type: "input", Content: "later"})
		if err != nil {
			t.Fatal(err)
		}
		read := func(c *ConversationHandle, definition *DocumentDefinition) JSON {
			t.Helper()
			value, found, err := c.SnapshotDefinition(ctx, definition, nil)
			if err != nil || !found {
				t.Fatal("fork fixture document", c.ID(), value, found, err)
			}
			return generationObject(value.Copy())
		}
		parentLive := read(c, LiveDoc())
		run := generationObject(parentLive["run"])
		generationID := submissionEntryID(run["taskId"])
		children, _ := generationObject(run["afterRun"])["tasks"].([]any)
		if generationID == 0 || len(children) != 1 {
			t.Fatal("expected the parent's one held after-run child", parentLive)
		}
		childID := submissionEntryID(children[0])
		assertParentWork := func() {
			t.Helper()
			if child, found, err := h.Task(ctx, childID); err != nil || !found || child.Owner != generationID || child.Conversation != c.ID() || child.State.Status != "running" {
				t.Fatal("parent lost its held after-run child", child, found, err)
			}
			if status, err := later.Status(ctx); err != nil || status.Status != "queued" || status.Entry != 0 {
				t.Fatal("parent input is not queued behind after-run", status, err)
			}
		}
		assertParentWork()
		parentInbox := read(c, InboxDoc())
		items, _ := parentInbox["items"].([]any)
		if len(items) != 1 || submissionEntryID(generationObject(items[0])["id"]) != later.ID() {
			t.Fatal("parent inbox lacks exactly the later input", parentInbox)
		}

		fork, err := c.Fork(ctx, settled.Record.Answer)
		if err != nil {
			t.Fatal(err)
		}
		forkLive := read(fork, LiveDoc())
		if _, present := forkLive["run"]; present {
			t.Fatal("fork copied active parent run", forkLive)
		}
		if inbox := read(fork, InboxDoc()); inbox["items"] != nil && !equalJSONValue(inbox["items"], []any{}) {
			t.Fatal("fork copied parent inbox", inbox)
		}
		if record, found, err := b.store.Conversation(ctx, fork.ID()); err != nil || !found || record.Owner != 0 || record.OwnerConversation != 0 || record.Parent != c.ID() || record.ParentAt != settled.Record.Answer {
			t.Fatal("expected ownerless fork at the committed answer", record, found, err)
		}
		if err := fork.WaitForIdle(ctx); err != nil {
			t.Fatal("fork did not start idle", err)
		}
		// A separate public wait for the still-held parent must remain pending.
		idleParent := awaitPendingContextIdleWait(t, ctx, h, c)
		assertParentWork()
		if !equalJSONValue(read(c, LiveDoc()), parentLive) || !equalJSONValue(read(c, InboxDoc()), parentInbox) || p.tasks.Load() != 1 || p.calls.Load() != 1 {
			t.Fatal("fork changed the parent run or queued input", p.tasks.Load(), p.calls.Load())
		}
		releaseTaskGate(gate)
		if err := watchReceive(t, idleParent); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
