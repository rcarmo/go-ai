package durable

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskOwnershipPinnedReopenedOwnedConversationInspectionAndIdle(t *testing.T) {
	for _, variant := range []string{"marked", "foreground-hold"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				checkReopenedOwnedInspectIdle(t, b.store, func() Storage { return reopenStoreAfterHarnessClose(t, b.store) }, variant)
			})
			t.Run("sqlite", func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "session.sqlite")
				store, err := OpenSQLite(path, SQLiteOptions{})
				if err != nil {
					t.Fatal(err)
				}
				checkReopenedOwnedInspectIdle(t, store, func() Storage {
					store, err := OpenSQLite(path, SQLiteOptions{})
					if err != nil {
						t.Fatal(err)
					}
					return store
				}, variant)
			})
		})
	}
}

func checkReopenedOwnedInspectIdle(t *testing.T, store Storage, reopen func() Storage, variant string) {
	entered, release := make(chan struct{}), make(chan struct{})
	var childRuns, childAborts, parentAborts atomic.Int64
	child := taskDefinition(t, "task.reopened-edge.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		childRuns.Add(1)
		close(entered)
		<-release
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
	})
	child.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		childAborts.Add(1)
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}
	parent := taskDefinition(t, "task.reopened-edge.parent", func(context.Context, TaskRecord, *TaskRuntime) error {
		return fmt.Errorf("replayed decided/marked owner")
	})
	parent.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		parentAborts.Add(1)
		if childAborts.Load() != 1 {
			return fmt.Errorf("abort order")
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}
	h := taskTestHarness(t, store, parent, child)
	cleanupTaskGates(t, release)
	parentID := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	var owned, childID ID
	_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
		var err error
		owned, err = tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.CreateConversation(Conversation{ID: owned, Owner: parentID}); err != nil {
			return err
		}
		childID, err = tx.CreateTask(child, nil, TaskOptions{Conversation: owned, Ownership: TaskOwnership{Kind: "conversation"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.session.Commit(bg, func(tx *Tx) error {
		state := TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}}
		if variant == "foreground-hold" {
			state = *taskDone("held")
			state.Status = "completing"
		}
		return seedNativeRecoveryState(tx, parentID, state, variant == "marked")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	h = taskTestHarness(t, reopen(), parent, child)
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	view, err := h.Inspect(bg)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, task := range view.Tasks {
		if task.Record.ID == parentID {
			if variant == "marked" {
				found = task.Kind == "waiting" && len(task.On) == 1 && task.On[0] == childID
			} else {
				found = task.Kind == "completing"
			}
		}
	}
	after, err := h.Snapshot(bg)
	if err != nil || before.Seq != after.Seq || view.Scheduling != "paused" || !found || childRuns.Load() != 0 || childAborts.Load() != 0 || parentAborts.Load() != 0 {
		t.Fatal("paused edge inspection", view, before.Seq, after.Seq, err)
	}
	if variant == "marked" {
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		final := waitPublicTask(t, h, parentID)
		if final.State.Outcome.Status != "aborted" || childAborts.Load() != 1 || parentAborts.Load() != 1 || childRuns.Load() != 0 {
			t.Fatal(final, childAborts.Load(), parentAborts.Load(), childRuns.Load())
		}
		return
	}
	caller, cancel := context.WithCancel(bg)
	waiting := make(chan error, 1)
	go func() { waiting <- h.WaitForIdle(caller) }()
	awaitTaskSignal(t, entered)
	// Admission is observed on the line; cancelling only the waiter must leave
	// the shared child and held outcome intact.
	registered := false
	deadline := time.After(3 * time.Second)
	for !registered {
		h.session.taskBookkeeping(func() {
			for waiter := range h.scheduler.waiters {
				if waiter.idleScope != nil {
					registered = true
				}
			}
		})
		select {
		case <-deadline:
			t.Fatal("idle registration absent")
		default:
		}
	}
	select {
	case err := <-waiting:
		t.Fatal("root idle ignored reopened owner edge", err)
	default:
	}
	cancel()
	if err := <-waiting; err != context.Canceled {
		t.Fatal(err)
	}
	record, ok, err := h.Task(bg, childID)
	if err != nil || !ok || record.AbortRequested || record.State.Status != "running" {
		t.Fatal("cancelled idle cancelled work", record, err)
	}
	releaseTaskGate(release)
	final := waitPublicTask(t, h, parentID)
	if final.State.Outcome.Status != "completed" || final.State.Outcome.Result.Value != "held" || childRuns.Load() != 1 || parentAborts.Load() != 0 {
		t.Fatal(final, childRuns.Load(), parentAborts.Load())
	}
	if err := h.WaitForIdle(bg); err != nil {
		t.Fatal(err)
	}
}
