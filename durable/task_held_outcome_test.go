package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipPinnedHeldFailureAndSchedulerFaultDrainBeforeParent(t *testing.T) {
	for _, ending := range []string{"failed", "faulted"} {
		t.Run(ending, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				runEntered, runRelease, abortEntered, abortRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, runRelease, abortRelease)
				var childID ID
				var parentAborts, childAborts atomic.Int64
				child := taskDefinition(t, "task.held-outcome.child", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
					close(runEntered)
					<-runRelease
					return ctx.Err()
				})
				child.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childAborts.Add(1)
					close(abortEntered)
					<-abortRelease
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "child"}}, nil
					})
				}
				parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.held-outcome.parent", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { parentAborts.Add(1); return nil }, Phases: map[string]TaskPhase{
					"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
							var err error
							childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
							if err != nil {
								return nil, err
							}
							return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish"}}, nil
						})
					},
					"finish": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						<-runEntered
						if ending == "faulted" {
							return errors.New("actual parent phase throw")
						}
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "parent declined"}}}, nil
						})
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
				awaitTaskSignal(t, runEntered)
				held := observeTaskState(t, h, id, "completing")
				if held.State.Outcome.Status != ending || held.AbortRequested {
					t.Fatal("held outcome", held)
				}
				close(runRelease)
				awaitTaskSignal(t, abortEntered)
				stillHeld, ok, err := h.Task(bg, id)
				if err != nil || !ok || stillHeld.State.Status != "completing" || stillHeld.State.Outcome.Status != ending || parentAborts.Load() != 0 {
					t.Fatal("parent completed before child abort actual return", stillHeld, err)
				}
				close(abortRelease)
				childRecord := waitPublicTask(t, h, childID)
				final := waitPublicTask(t, h, id)
				if childRecord.State.Outcome.Status != "aborted" || final.State.Outcome.Status != ending || final.AbortRequested || parentAborts.Load() != 0 || childAborts.Load() != 1 {
					t.Fatal("drained held outcome", final, childRecord, parentAborts.Load(), childAborts.Load())
				}
			})
		})
	}
}
