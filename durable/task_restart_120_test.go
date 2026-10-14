package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestTask120AbandonOnRestartMissingDefinitionWaitsForCleanup(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var runs, aborts atomic.Int64
		definition, e := DefineTask(TaskDefinitionOptions{Kind: "restart-disposable", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil }, Phases: map[string]TaskPhase{"work": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runs.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("ran"), nil })
		}}, Abort: func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			if record.AbortReason != "restart" {
				t.Error("reason lost", record.AbortReason)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "restart"}}, nil
			})
		}})
		if e != nil {
			t.Fatal(e)
		}
		first := taskTestHarness(t, b.store, definition)
		id := createPublicTask(t, first, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, AbandonOnRestart: true})
		if e = first.Close(bg); e != nil {
			t.Fatal(e)
		}
		second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store))
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
			task := state.Tasks[id]
			if taskAborted(task) {
				if task.AbortReason != "restart" || terminalStatus(task.Status) {
					t.Fatal("orphaned restart task", task)
				}
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("restart mark missing")
			case <-time.After(time.Millisecond):
			}
		}
		if runs.Load() != 0 || aborts.Load() != 0 {
			t.Fatal("dispatch without definition", runs.Load(), aborts.Load())
		}
		if _, e = second.options.Registry.RegisterTask(definition); e != nil {
			t.Fatal(e)
		}
		result, e := second.WaitForTask(deadline, id)
		if e != nil || result.State.Outcome == nil || result.State.Outcome.Status != "aborted" || aborts.Load() != 1 || runs.Load() != 0 {
			t.Fatal(result, e, runs.Load(), aborts.Load())
		}
	})
}
func TestTask120DisposableDoesNotAbandonNewTasks(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		definition := taskDefinition(t, "restart-new", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("normal"), nil })
		})
		h := taskTestHarness(t, b.store, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, AbandonOnRestart: true})
		result := waitPublicTask(t, h, id)
		if result.State.Outcome.Status != "completed" || !result.AbandonOnRestart || result.AbortReason != "" {
			t.Fatal(result)
		}
	})
}
