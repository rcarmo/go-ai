package durable

import (
	"context"
	"testing"
)

func TestTaskOwnershipPinnedTerminalOrdinaryOwnerDoesNotRecascadeNewConversationWork(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		owner := taskDefinition(t, "task.terminal-owner.parent", func(context.Context, TaskRecord, *TaskRuntime) error {
			t.Error("marked owner ran ordinary phase")
			return nil
		})
		entered, release := make(chan struct{}), make(chan struct{})
		child := taskDefinition(t, "task.terminal-owner.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("later"), nil })
		})
		h := taskTestHarness(t, b.store, owner, child)
		cleanupTaskGates(t, release)
		ownerID := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		var owned ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			owned, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: owned, Owner: ownerID})
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.AbortTask(bg, ownerID); err != nil {
			t.Fatal(err)
		}
		if final := waitPublicTask(t, h, ownerID); final.State.Outcome.Status != "aborted" {
			t.Fatal(final)
		}
		var childID ID
		_, err = h.CommitTasks(bg, owned, func(tx *Tx) error {
			var err error
			childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		task, ok, err := h.Task(bg, childID)
		if err != nil || !ok || task.AbortRequested || task.State.Status != "running" {
			t.Fatal("terminal owner recascaded", task, err)
		}
		releaseTaskGate(release)
		if final := waitPublicTask(t, h, childID); final.State.Outcome.Status != "completed" || final.State.Outcome.Result.Value != "later" {
			t.Fatal(final)
		}
	})
}
