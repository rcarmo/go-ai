package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// harness-tasks-recovery.test.ts: startedAt survives the interrupted apply and
// close; endedAt is stamped by the second runtime when the resumed task settles.
func TestSourceV152SQLiteTaskTimesSurviveClose(t *testing.T) {
	path := copySQLiteV152Fixture(t, "schema-v1.sqlite")
	var clock atomic.Int64
	clock.Store(2_000)
	var calls atomic.Int64
	applied := make(chan struct{})
	transfer, err := DefineTask(TaskDefinitionOptions{
		Kind: "test.transfer", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "prepare"}, nil },
		Phases: map[string]TaskPhase{
			"prepare": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				amount := task.Input.Value.(map[string]any)["amount"]
				if _, err := r.MemoCandidate(ctx, "requested", amount); err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "apply", "key": fmt.Sprintf("transfer-%d", task.ID)}}, nil
				})
			},
			"apply": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				if !equalJSONValue(task.Input.Value, JSON{"amount": 7}) || !equalJSONValue(task.State.Checkpoint, JSON{"phase": "apply", "key": fmt.Sprintf("transfer-%d", task.ID)}) {
					return fmt.Errorf("wrong resumed state: %+v", task)
				}
				if calls.Add(1) == 1 {
					close(applied)
					<-ctx.Done()
					return ctx.Err()
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(JSON{"receipt": 70}), nil })
			},
		},
		Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if _, err := registry.RegisterTask(transfer); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	open := func() *Harness {
		t.Helper()
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		h, err := Open(ctx, store, Options{Registry: registry, Now: clock.Load})
		if err != nil {
			_ = store.Close(bg)
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = h.Close(bg) })
		return h
	}
	first := open()
	root, err := first.Root(ctx, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := root.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(transfer, JSON{"amount": 7}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := value.(ID)
	if err := first.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-applied:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	clock.Store(3_000)
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	clock.Store(9_000)
	second := open()
	pending, found, err := second.Task(ctx, id)
	if err != nil || !found || pending.State.Status != "pending" || pending.StartedAt == nil || *pending.StartedAt != 2_000 || pending.EndedAt != nil {
		t.Fatal(pending, found, err)
	}
	if err := second.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	terminal, err := second.WaitForTask(ctx, id)
	if err != nil || terminal.StartedAt == nil || *terminal.StartedAt != 2_000 || terminal.EndedAt == nil || *terminal.EndedAt != 9_000 {
		t.Fatal(terminal, err)
	}
	if terminal.State.Status != "terminal" || terminal.State.Outcome == nil || terminal.State.Outcome.Status != "completed" || terminal.State.Outcome.Result == nil || !equalJSONValue(terminal.State.Outcome.Result.Value, JSON{"receipt": 70}) || calls.Load() != 2 {
		t.Fatal(terminal.State, calls.Load())
	}
	if err := second.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
