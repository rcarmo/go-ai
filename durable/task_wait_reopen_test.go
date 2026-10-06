package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskOwnershipPinnedWaitingTwoChildrenCloseReopenOrderedOutcomes(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		checkWaitingChildrenReopen(t, b, func() Storage { return reopenStoreAfterHarnessClose(t, b.store) })
	})
}

func checkWaitingChildrenReopen(t *testing.T, b backend, reopen func() Storage) {
	started := make(chan ID, 4)
	var generation atomic.Int64
	generation.Store(1)
	var children []ID
	var joined []TaskOutcome
	var parentRuns, childRuns atomic.Int64
	child := taskDefinition(t, "task.wait-reopen.payment", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		childRuns.Add(1)
		started <- r.TaskID()
		if generation.Load() == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(fmt.Sprint(r.TaskID())), nil })
	})
	parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.wait-reopen.checkout", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
		"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			parentRuns.Add(1)
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				for i := 0; i < 2; i++ {
					id, err := tx.CreateTask(child, i, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
					if err != nil {
						return nil, err
					}
					children = append(children, id)
				}
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: children, Policy: "failFast"}, nil
			})
		},
		"join": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			var err error
			joined, err = r.Outcomes(ctx, children)
			if err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("paid"), nil })
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, b.store, parent, child)
	id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("actual payment child did not start")
		}
	}
	waiting := observeTaskState(t, h, id, "waiting")
	if len(waiting.State.On) != 2 {
		t.Fatal(waiting)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	generation.Store(2)
	store := reopen()
	h = taskTestHarness(t, store, parent, child)
	persisted, ok, err := h.Task(bg, id)
	if err != nil || !ok || persisted.State.Status != "waiting" || len(persisted.State.On) != 2 {
		t.Fatal("waiting lost on reopen", persisted, ok, err)
	}
	children = append([]ID(nil), persisted.State.On...)
	if parentRuns.Load() != 1 || childRuns.Load() != 2 {
		t.Fatal("reopen dispatched before progress", parentRuns.Load(), childRuns.Load())
	}
	select {
	case unexpected := <-started:
		t.Fatal("reopen started child before progress", unexpected)
	default:
	}
	final := waitPublicTask(t, h, id)
	if final.State.Outcome.Status != "completed" || final.State.Outcome.Result.Value != "paid" || len(joined) != 2 || parentRuns.Load() != 1 || childRuns.Load() != 4 {
		t.Fatal("recovery replayed spawn or lost payment", final, joined, parentRuns.Load(), childRuns.Load())
	}
	for i, outcome := range joined {
		if outcome.Status != "completed" || outcome.Result.Value != fmt.Sprint(children[i]) {
			t.Fatal("ordered recovered outcomes", joined, children)
		}
	}
	state, err := h.Snapshot(bg)
	if err != nil || len(state.Tasks) != 3 {
		t.Fatal("duplicate recovered work", len(state.Tasks), err)
	}
}
