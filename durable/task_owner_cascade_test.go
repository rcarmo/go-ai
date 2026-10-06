package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipFailedOwnerCascadesButCompletedOwnerDrainsNormally(t *testing.T) {
	for _, ending := range []string{"failed", "completed"} {
		t.Run(ending, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				childStarted, decide, complete := make(chan struct{}), make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, decide, complete)
				var childID ID
				var childAborts atomic.Int64
				child := taskDefinition(t, "task.cascade.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					close(childStarted)
					select {
					case <-complete:
					case <-ctx.Done():
						return ctx.Err()
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				child.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childAborts.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				owner, err := DefineTask(TaskDefinitionOptions{Kind: "task.cascade.owner", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "create"}, nil }, Phases: map[string]TaskPhase{
					"create": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
							conversation, err := tx.MintID()
							if err != nil {
								return nil, err
							}
							if err := tx.CreateConversation(Conversation{ID: conversation, Owner: r.TaskID()}); err != nil {
								return nil, err
							}
							childID, err = tx.CreateTask(child, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "conversation"}})
							if err != nil {
								return nil, err
							}
							return &TaskState{Status: "running", Checkpoint: JSON{"phase": "decide"}}, nil
						})
					},
					"decide": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						<-decide
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							if ending == "failed" {
								return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "owner failed"}}}, nil
							}
							return taskDone(nil), nil
						})
					},
				}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}})
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, owner, child)
				id := createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, childStarted)
				releaseTaskGate(decide)
				if ending == "completed" {
					record := observeTaskState(t, h, id, "completing")
					childRecord, ok, err := h.Task(bg, childID)
					if record.State.Outcome.Status != "completed" || err != nil || !ok || childRecord.AbortRequested || childAborts.Load() != 0 {
						t.Fatal(record, childRecord, err)
					}
					releaseTaskGate(complete)
				}
				record := waitPublicTask(t, h, id)
				childRecord := waitPublicTask(t, h, childID)
				wantChild, wantAborts := "completed", int64(0)
				if ending == "failed" {
					wantChild, wantAborts = "aborted", 1
				}
				if record.State.Outcome.Status != ending || childRecord.State.Outcome.Status != wantChild || childAborts.Load() != wantAborts {
					t.Fatal(record, childRecord, childAborts.Load())
				}
			})
		})
	}
}
