package durable

import (
	"context"
	"testing"
	"time"
)

func TestTask120ExplicitAbortSupersedesRestartBlockedChild(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var parent, child ID
		parentDef := taskDefinition(t, "restart-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{child}, Policy: "allSettled"}, nil
			})
		})
		childDef := taskDefinition(t, "restart-child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("unused"), nil })
		})
		first := taskTestHarness(t, b.store, parentDef, childDef)
		_, e := first.CommitTasks(bg, 1, func(tx *Tx) error {
			var e error
			parent, e = tx.CreateTask(parentDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if e != nil {
				return e
			}
			child, e = tx.CreateTask(childDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}, AbandonOnRestart: true})
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		if e = first.Close(bg); e != nil {
			t.Fatal(e)
		}
		second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), parentDef)
		if e = second.Resume(bg); e != nil {
			t.Fatal(e)
		}
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			state, e := second.Snapshot(deadline)
			if e != nil {
				t.Fatal(e)
			}
			if state.Tasks[child].AbortReason == "restart" {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("restart mark absent")
			case <-time.After(time.Millisecond):
			}
		}
		if _, e = second.AbortTask(deadline, parent); e != nil {
			t.Fatal(e)
		}
		result, e := second.WaitForTask(deadline, child)
		if e != nil || result.State.Outcome == nil || result.State.Outcome.Status != "orphaned" || result.AbortReason != "" {
			t.Fatal(result, e)
		}
		result, e = second.WaitForTask(deadline, parent)
		if e != nil || result.State.Outcome == nil || result.State.Outcome.Status != "aborted" {
			t.Fatal(result, e)
		}
	})
}
