package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskRecoveryIntentEffectOutcomeIdempotencyCloseReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		applied := map[string]int{}
		var calls atomic.Int64
		started, release := make(chan struct{}), make(chan struct{})
		transfer := taskDefinition(t, "task.reference.transfer", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			key, found, err := r.Memo(ctx, "key")
			if err != nil {
				return err
			}
			if !found {
				key, err = r.MemoCandidate(ctx, "key", "transfer-1")
				if err != nil {
					return err
				}
			}
			calls.Add(1)
			text := key.(string)
			// The remote host owns idempotency. An acknowledged durable intent can
			// repeat the host call; the same memo key avoids repeating the effect.
			if _, ok := applied[text]; !ok {
				applied[text] = 70
			}
			if calls.Load() == 1 {
				close(started)
				<-release
				return ctx.Err()
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(applied[text]), nil })
		})
		h := taskTestHarness(t, b.store, transfer)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, transfer, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		// Close interrupts the real host after its effect but before its receipt.
		closing := make(chan error, 1)
		go func() { closing <- h.Close(bg) }()
		awaitTaskSignal(t, h.life.Done())
		releaseTaskGate(release)
		select {
		case err := <-closing:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("close did not join transfer")
		}
		h2 := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), transfer)
		record, found, err := h2.Task(bg, id)
		if err != nil || !found || record.State.Status != "pending" || calls.Load() != 1 {
			t.Fatal(record, found, err, calls.Load())
		}
		final := waitPublicTask(t, h2, id)
		if final.State.Outcome == nil || final.State.Outcome.Status != "completed" || !equalJSONValue(final.State.Outcome.Result.Value, 70) || len(applied) != 1 || calls.Load() != 2 {
			t.Fatal(final, applied, calls.Load())
		}
		if err := h2.Close(bg); err != nil {
			t.Fatal(err)
		}
		h3 := taskTestHarness(t, reopenStoreAfterHarnessClose(t, h2.session.store), transfer)
		again := waitPublicTask(t, h3, id)
		if !equalJSONValue(again.State.Outcome, final.State.Outcome) || calls.Load() != 2 {
			t.Fatal("terminal effect repeated", again, calls.Load())
		}
	})
}

func TestTaskOwnershipLiveFailedOwnerAbortsNewWorkWhileOldChildDrains(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		oldAbort, releaseAbort, ownerHeld := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var ownedConversation ID
		var normal, aborted atomic.Int64
		child := taskDefinition(t, "task.reference.live.failed.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			normal.Add(1)
			<-ctx.Done()
			return ctx.Err()
		})
		child.options.Abort = func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if task.Input.Value == "old" {
				close(oldAbort)
				<-releaseAbort
			}
			aborted.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		owner := taskDefinition(t, "task.reference.live.failed.owner", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(tx *Tx, task TaskRecord) (*TaskState, error) {
				var err error
				ownedConversation, err = tx.MintID()
				if err != nil {
					return nil, err
				}
				if err = tx.CreateConversation(Conversation{ID: ownedConversation, Owner: task.ID}); err != nil {
					return nil, err
				}
				_, err = tx.CreateTask(child, "old", TaskOptions{Conversation: ownedConversation, Ownership: TaskOwnership{Kind: "conversation"}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "fail"}}, nil
			}); err != nil {
				return err
			}
			return nil
		})
		owner.options.Phases["fail"] = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "failed"}}}, nil
			})
			close(ownerHeld)
			return err
		}
		h := taskTestHarness(t, b.store, owner, child)
		cleanupTaskGates(t, releaseAbort)
		id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, ownerHeld)
		awaitTaskSignal(t, oldAbort)
		current, ok, err := h.Task(bg, id)
		if err != nil || !ok || current.State.Status != "completing" {
			t.Fatal("owner not held", current, err)
		}
		var late ID
		_, err = h.CommitTasks(bg, ownedConversation, func(tx *Tx) error {
			var err error
			late, err = tx.CreateTask(child, "late", TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		lateResult := waitPublicTask(t, h, late)
		if lateResult.State.Outcome.Status != "aborted" {
			t.Fatal(lateResult)
		}
		// The late ordinary handler must not dispatch beneath a failed live owner.
		if normal.Load() > 1 {
			t.Fatal("late child ran", normal.Load())
		}
		releaseTaskGate(releaseAbort)
		final := waitPublicTask(t, h, id)
		if final.State.Outcome.Status != "failed" || aborted.Load() != 2 {
			t.Fatal(final, aborted.Load())
		}
		future := taskDefinition(t, "task.reference.live.failed.future", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("future"), nil })
		})
		if _, err := h.options.Registry.RegisterTask(future); err != nil {
			t.Fatal(err)
		}
		var futureID ID
		_, err = h.CommitTasks(bg, ownedConversation, func(tx *Tx) error {
			var err error
			futureID, err = tx.CreateTask(future, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if futureResult := waitPublicTask(t, h, futureID); futureResult.State.Outcome.Status != "completed" {
			t.Fatal("terminal owner recascaded", futureResult)
		}
	})
}

func TestTaskOwnershipActiveBackgroundOwnerStopsRootIdleButOwnedScopeRemainsBusy(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		started, release := make(chan struct{}), make(chan struct{})
		var conversation, childID ID
		child := taskDefinition(t, "task.reference.background.scope.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
		owner := taskDefinition(t, "task.reference.background.scope.owner", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(tx *Tx, task TaskRecord) (*TaskState, error) {
				var err error
				conversation, err = tx.MintID()
				if err != nil {
					return nil, err
				}
				if err = tx.CreateConversation(Conversation{ID: conversation, Owner: task.ID}); err != nil {
					return nil, err
				}
				childID, err = tx.CreateTask(child, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "conversation"}})
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "work"}}, err
			}); err != nil {
				return err
			}
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("owner"), nil })
		})
		h := taskTestHarness(t, b.store, owner, child)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := h.WaitForIdle(ctx); err != nil {
			t.Fatal("background blocked root idle", err)
		}
		c, err := h.Conversation(bg, conversation)
		if err != nil {
			t.Fatal(err)
		}
		// A cancelled scope wait observes busy work without altering it.
		busy, cancelBusy := context.WithCancel(bg)
		cancelBusy()
		if err := c.WaitForIdle(busy); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		root, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		if err := root.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		before, err := h.Snapshot(bg)
		if err != nil || taskAborted(before.Tasks[id]) || taskAborted(before.Tasks[childID]) {
			t.Fatal("root crossed background edge", err)
		}
		if err := c.Abort(ctx); err != nil {
			t.Fatal(err)
		}
		final := waitPublicTask(t, h, childID)
		if final.State.Outcome.Status != "aborted" {
			t.Fatal(final)
		}
		current, ok, err := h.Task(bg, id)
		if err != nil || !ok || current.State.Status == "terminal" || current.AbortRequested {
			t.Fatal("child abort selected owner", current, err)
		}
		releaseTaskGate(release)
		waitPublicTask(t, h, id)
	})
}
