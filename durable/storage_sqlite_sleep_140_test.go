//go:build cgo

package durable

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// Native SQLite stores validated frames/checkpoints rather than the upstream
// status-column schema. Exercise sleeping through both persistence paths and
// namespaces instead of copying the upstream table-rebuild migration.
func TestSQLite140SleepingTaskReopenAndReclaim(t *testing.T) {
	for _, prefix := range []string{"", "audit_"} {
		for _, reclaim := range []bool{false, true} {
			name := prefix + "frames"
			if reclaim {
				name = prefix + "checkpoint"
			}
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "session.sqlite")
				var now atomic.Int64
				now.Store(1000)
				var effects atomic.Int64
				def, err := DefineTask(TaskDefinitionOptions{Kind: "task.sqlite-sleep-140", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "sleep"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
					"sleep": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						return r.SleepUntil(ctx, 2000, JSON{"phase": "finish"})
					},
					"finish": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						effects.Add(1)
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("awake"), nil })
					},
				}})
				if err != nil {
					t.Fatal(err)
				}
				open := func() *SQLiteStorage {
					store, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
					if err != nil {
						t.Fatal(err)
					}
					return store
				}
				opts := Options{Now: now.Load, OnWake: func(Wake) {}}
				h := taskTestHarnessOptions(t, open(), opts, def)
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				observeTaskState(t, h, id, "sleeping")
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				if reclaim {
					store := open()
					if err := store.Reclaim(bg); err != nil {
						t.Fatal(err)
					}
					if err := store.Close(bg); err != nil {
						t.Fatal(err)
					}
				}
				h = taskTestHarnessOptions(t, open(), opts, def)
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				state := observeTaskState(t, h, id, "sleeping")
				if state.State.Until == nil || *state.State.Until != 2000 || effects.Load() != 0 {
					t.Fatal(state, effects.Load())
				}
				now.Store(2000)
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				result := waitPublicTask(t, h, id)
				if result.State.Outcome == nil || result.State.Outcome.Status != "completed" || effects.Load() != 1 {
					t.Fatal(result, effects.Load())
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
