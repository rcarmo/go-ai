package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipCancelledOwnerLateChildConversationWorkAfterReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		checkCancelledOwnerLateWorkReopen(t, b.store, func() Storage { return reopenStoreAfterHarnessClose(t, b.store) })
	})
}

func checkCancelledOwnerLateWorkReopen(t *testing.T, store Storage, reopen func() Storage) {
	abortEntered := make(chan struct{}, 2)
	releaseAbort := make(chan struct{})
	created := make(chan struct{})
	cleanupTaskGates(t, releaseAbort)
	var childConversation, inner ID
	var lateCalls atomic.Int64
	late := taskDefinition(t, "task.late.reopen", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		lateCalls.Add(1)
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	done := taskDefinition(t, "task.late.inner", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	owner, err := DefineTask(TaskDefinitionOptions{Kind: "task.late.owner", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "create"}, nil }, Phases: map[string]TaskPhase{
		"create": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				childConversation, err = tx.MintID()
				if err != nil {
					return nil, err
				}
				if err := tx.CreateConversation(Conversation{ID: childConversation, Owner: r.TaskID()}); err != nil {
					return nil, err
				}
				inner, err = tx.CreateTask(done, nil, TaskOptions{Conversation: childConversation, Ownership: TaskOwnership{Kind: "conversation"}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "hold"}}, nil
			})
			if err == nil {
				close(created)
			}
			return err
		},
		"hold": func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error { <-ctx.Done(); return ctx.Err() },
	}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		abortEntered <- struct{}{}
		select {
		case <-releaseAbort:
		case <-ctx.Done():
			return ctx.Err()
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, store, owner, done, late)
	id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, created)
	waitPublicTask(t, h, inner)
	if marked, err := h.AbortTask(bg, id); err != nil || marked != "marked" {
		t.Fatal(marked, err)
	}
	awaitTaskSignal(t, abortEntered)
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	h = taskTestHarness(t, reopen(), owner, done, late)
	view, err := h.Inspect(bg)
	if err != nil || view.Scheduling != "paused" {
		t.Fatal(view, err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var lateID ID
	_, err = h.CommitTasks(bg, childConversation, func(tx *Tx) error {
		var err error
		lateID, err = tx.CreateTask(late, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err != nil {
		t.Fatal("late child admission", err)
	}
	if record := waitPublicTask(t, h, lateID); record.State.Outcome.Status != "aborted" || lateCalls.Load() != 0 {
		t.Fatal("late work escaped cancelled owner", record, lateCalls.Load())
	}
	awaitTaskSignal(t, abortEntered)
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq <= before.Seq {
		t.Fatal(after.Seq, err)
	}
	releaseTaskGate(releaseAbort)
	if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "aborted" {
		t.Fatal(record)
	}
}
