package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipPinnedOwnerMarkWinsDependencyCompletionSameCommit(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ownerEntered, dependencyEntered, dependencyRelease, waitingCommitted := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var joins, aborts atomic.Int64
		var dependencyID ID
		owner := taskDefinition(t, "task.mark-dependency.owner", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(ownerEntered)
			<-ctx.Done()
			return ctx.Err()
		})
		dependency := taskDefinition(t, "task.mark-dependency.dependency", func(context.Context, TaskRecord, *TaskRuntime) error {
			close(dependencyEntered)
			<-dependencyRelease
			return nil
		})
		waiter, err := DefineTask(TaskDefinitionOptions{Kind: "task.mark-dependency.waiter", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "wait"}, nil }, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}, Phases: map[string]TaskPhase{
			"wait": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{dependencyID}, Policy: "allSettled"}, nil
				})
				close(waitingCommitted)
				return err
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				joins.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("wrong"), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, owner, dependency, waiter)
		cleanupTaskGates(t, dependencyRelease)
		ownerID := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		dependencyID = createPublicTask(t, h, dependency, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		var owned, waiterID ID
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			owned, err = tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.CreateConversation(Conversation{ID: owned, Owner: ownerID}); err != nil {
				return err
			}
			waiterID, err = tx.CreateTask(waiter, nil, TaskOptions{Conversation: owned, Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, ownerEntered)
		awaitTaskSignal(t, dependencyEntered)
		awaitTaskSignal(t, waitingCommitted)
		observeTaskState(t, h, waiterID, "waiting")
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			if err := seedNativeRecoveryState(tx, dependencyID, *taskDone("settled"), false); err != nil {
				return err
			}
			task := markTask(tx.state.Tasks[ownerID])
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(dependencyRelease)
		final := waitPublicTask(t, h, waiterID)
		if final.State.Outcome.Status != "aborted" || aborts.Load() != 1 || joins.Load() != 0 {
			t.Fatal("dependency completion resumed cancelled subtree", final, aborts.Load(), joins.Load())
		}
		if final := waitPublicTask(t, h, ownerID); final.State.Outcome.Status != "aborted" {
			t.Fatal(final)
		}
	})
}
