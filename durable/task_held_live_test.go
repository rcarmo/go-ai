package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipPinnedHeldOutcomeVersionAndLiveOutcomes(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release, heldReturn, probe, probed, waitCommitted := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		var parentRuns, migrations, replacementRuns atomic.Int64
		child := taskDefinition(t, "task.held-live.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
		})
		parent := taskDefinition(t, "task.held-live.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			parentRuns.Add(1)
			if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				_, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				return nil, err
			}); err != nil {
				return err
			}
			<-entered
			err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("held"), nil })
			close(heldReturn)
			return err
		})
		var parentID ID
		var joined []TaskOutcome
		observer, err := DefineTask(TaskDefinitionOptions{Kind: "task.held-live.observer", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "probe"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
			"probe": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				<-probe
				if _, err := r.Outcomes(ctx, []ID{parentID}); err == nil {
					return fmt.Errorf("held task reported terminal outcome")
				}
				close(probed)
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{parentID}, Policy: "allSettled"}, nil
				})
				close(waitCommitted)
				return err
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				var err error
				joined, err = r.Outcomes(ctx, []ID{parentID})
				if err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("observed"), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		reports := make(chan error, 8)
		h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) { reports <- err }}, parent, child, observer)
		cleanupTaskGates(t, release, probe)
		parentID = createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		observerID := createPublicTask(t, h, observer, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, heldReturn)
		held := observeTaskState(t, h, parentID, "completing")
		options := parent.options
		options.Version = 2
		options.Migrate = func(input any, checkpoint JSON, _ uint64) (any, JSON, error) {
			migrations.Add(1)
			return input, checkpoint, nil
		}
		options.Phases = map[string]TaskPhase{"work": func(context.Context, TaskRecord, *TaskRuntime) error {
			replacementRuns.Add(1)
			return fmt.Errorf("held task reran")
		}}
		replacement, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.options.Registry.RegisterTask(replacement); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(probe)
		awaitTaskSignal(t, probed)
		awaitTaskSignal(t, waitCommitted)
		waiting := observeTaskState(t, h, observerID, "waiting")
		if len(waiting.State.On) != 1 || waiting.State.On[0] != parentID {
			t.Fatal(waiting)
		}
		releaseTaskGate(release)
		final := waitPublicTask(t, h, parentID)
		waitPublicTask(t, h, observerID)
		if final.Version != held.Version || final.Version != 1 || final.State.Outcome.Status != "completed" || final.State.Outcome.Result.Value != "held" || migrations.Load() != 0 || replacementRuns.Load() != 0 || parentRuns.Load() != 1 {
			t.Fatal("held task migrated/replayed", held, final, migrations.Load(), replacementRuns.Load(), parentRuns.Load())
		}
		if len(joined) != 1 || joined[0].Status != "completed" || joined[0].Result.Value != "held" {
			t.Fatal(joined)
		}
		select {
		case report := <-reports:
			t.Fatal("held replacement reported", report)
		default:
		}
	})
}
