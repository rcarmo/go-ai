package durable

import (
	"context"
	"errors"
	"testing"
)

func TestTask120OwnedTasksDetachedDirectAndAbortAuthority(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		childDef := taskDefinition(t, "owned-child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
		})
		var escaped *TaskRuntime
		parentDef := taskDefinition(t, "owned-parent", func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
			escaped = r
			var child ID
			if e := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var e error
				child, e = tx.CreateTask(childDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: record.ID}})
				return nil, e
			}); e != nil {
				return e
			}
			tasks, e := r.OwnedTasks(ctx)
			if e != nil {
				return e
			}
			if len(tasks) != 1 || tasks[0].ID != child {
				t.Fatal(tasks)
			}
			tasks[0].State.Checkpoint["mutated"] = true
			again, e := r.OwnedTasks(ctx)
			if e != nil {
				return e
			}
			if again[0].State.Checkpoint["mutated"] != nil {
				t.Fatal("owned-task alias")
			}
			if e = r.AbortOwned(ctx, record.ID); e == nil {
				t.Fatal("self abort accepted as owned")
			}
			if e = r.AbortOwned(ctx, child); e != nil {
				return e
			}
			tasks, e = r.OwnedTasks(ctx)
			if e != nil {
				return e
			}
			if len(tasks) != 0 {
				t.Fatal("terminal child listed", tasks)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
		})
		h := taskTestHarness(t, b.store, parentDef, childDef)
		id := createPublicTask(t, h, parentDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		result := waitPublicTask(t, h, id)
		if result.State.Outcome.Status != "completed" {
			t.Fatal(result)
		}
		if _, e := escaped.OwnedTasks(bg); !errors.Is(e, ErrSealed) {
			t.Fatal("ended read authority", e)
		}
		if e := escaped.AbortOwned(bg, id); !errors.Is(e, ErrSealed) {
			t.Fatal("ended abort authority", e)
		}
	})
}
