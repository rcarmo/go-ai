package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskOwnershipPinnedMissingDefinitionWaitingOnLiveForeignOrphansAtAbort(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release, waiting := make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var foreignAborts, parentAborts atomic.Int64
		foreign := taskDefinition(t, "task.live-foreign", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("foreign"), nil })
		})
		foreign.options.Abort = func(context.Context, TaskRecord, *TaskRuntime) error { foreignAborts.Add(1); return nil }
		var foreignID ID
		parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.missing-waiter", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { parentAborts.Add(1); return nil }, Phases: map[string]TaskPhase{
			"run": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{foreignID}, Policy: "allSettled"}, nil
				}); err != nil {
					return err
				}
				close(waiting)
				return nil
			},
			"join": func(context.Context, TaskRecord, *TaskRuntime) error {
				t.Error("missing definition resumed")
				return nil
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, foreign)
		// Gates must release before the harness's LIFO Close cleanup on failures.
		cleanupTaskGates(t, release)
		dispose, err := h.options.Registry.RegisterTask(parent)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.options.Registry.RegisterTask(foreign); err != nil {
			t.Fatal(err)
		}
		foreignID = createPublicTask(t, h, foreign, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		parentID := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		awaitTaskSignal(t, waiting)
		var document ID
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			document, err = tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: document, Scope: "task", Owner: parentID, Kind: "app.missing-wait", Version: 1, Value: JSON{"kept": true}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		var returned chan struct{}
		h.session.taskBookkeeping(func() {
			if r := h.scheduler.invocations[parentID]; r != nil {
				returned = r.done
			}
		})
		if returned != nil {
			awaitTaskSignal(t, returned)
		}
		dispose()
		caller, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		result, err := h.AbortTask(caller, parentID)
		if err != nil || result != "marked" {
			t.Fatal(result, err)
		}
		after, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		view, ok, err := h.Task(bg, parentID)
		if err != nil || !ok || view.State.Status != "terminal" || view.State.Outcome.Status != "orphaned" || view.State.Outcome.Reason != "missing_task" || !after.Documents[document].Retired || after.Tasks[foreignID].Status != "running" || taskAborted(after.Tasks[foreignID]) || parentAborts.Load() != 0 || foreignAborts.Load() != 0 {
			t.Fatal("foreign wait became ownership", view, err, after.Tasks[foreignID], parentAborts.Load(), foreignAborts.Load())
		}
		if after.Documents[document].RetiredAt != after.Seq {
			t.Fatal("orphan retirement not atomic")
		}
		close(release)
		waitPublicTask(t, h, foreignID)
	})
}
