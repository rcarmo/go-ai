package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskOwnershipPinnedMarkCompletedHoldPreservesOutcomeAndDrainsChild(t *testing.T) {
	for _, scope := range []string{"task", "conversation"} {
		t.Run(scope, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				childEntered, childRelease, abortEntered, abortRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				var parentAborts, childAborts atomic.Int64
				child := taskDefinition(t, "task.held-mark.child", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
					close(childEntered)
					<-childRelease
					return ctx.Err()
				})
				child.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childAborts.Add(1)
					close(abortEntered)
					<-abortRelease
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "owner_mark"}}, nil
					})
				}
				var childID ID
				parent := taskDefinition(t, "task.held-mark.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						var err error
						childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
						return nil, err
					}); err != nil {
						return err
					}
					<-childEntered
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("kept_parent"), nil })
				})
				parent.options.Abort = func(context.Context, TaskRecord, *TaskRuntime) error { parentAborts.Add(1); return nil }
				h := taskTestHarness(t, b.store, parent, child)
				cleanupTaskGates(t, childRelease, abortRelease)
				parentID := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, childEntered)
				caller, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				for {
					state, err := h.Snapshot(caller)
					if err != nil {
						t.Fatal(err)
					}
					if state.Tasks[parentID].Status == "completing" {
						break
					}
				}
				marked := make(chan error, 1)
				go func() {
					if scope == "conversation" {
						root, err := h.Conversation(caller, 1)
						if err == nil {
							err = root.Abort(caller)
						}
						marked <- err
						return
					}
					result, err := h.AbortTask(caller, parentID)
					if result != "marked" && err == nil {
						t.Error(result)
					}
					marked <- err
				}()
				for {
					state, err := h.Snapshot(caller)
					if err != nil {
						t.Fatal(err)
					}
					if taskAborted(state.Tasks[childID]) {
						break
					}
				}
				close(childRelease)
				awaitTaskSignal(t, abortEntered)
				state, err := h.Snapshot(caller)
				if err != nil || state.Tasks[parentID].Status != "completing" || state.Tasks[parentID].Execution.Native.State.Outcome.Status != "completed" || parentAborts.Load() != 0 {
					t.Fatal("held outcome replaced", state.Tasks[parentID], err, parentAborts.Load())
				}
				close(abortRelease)
				if err := <-marked; err != nil {
					t.Fatal(err)
				}
				record := waitPublicTask(t, h, parentID)
				if record.State.Outcome.Status != "completed" || record.State.Outcome.Result.Value != "kept_parent" || !record.AbortRequested || parentAborts.Load() != 0 || childAborts.Load() != 1 {
					t.Fatal(record, parentAborts.Load(), childAborts.Load())
				}
				if result, err := h.AbortTask(bg, parentID); err != nil || result != "terminal" {
					t.Fatal(result, err)
				}
			})
		})
	}
}
