package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
)

func seedInterrupted140(t *testing.T, h *Harness, id ID) {
	t.Helper()
	_, err := h.session.Commit(bg, func(tx *Tx) error {
		task := tx.state.Tasks[id]
		task.Status = "running"
		native := *task.Execution.Native
		native.State.Status = "running"
		task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &native}
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestRestartLimit140RecoveryParkingRetryRaiseAndAbort(t *testing.T) {
	for _, action := range []string{"retry", "raise", "abort"} {
		t.Run(action, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var runs, reports atomic.Int64
				def := taskDefinition(t, "task.restart-limit-140", func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
					runs.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
				})
				limit := uint64(2)
				options := Options{Settings: &HarnessSettings{RestartLimit: &limit}, OnReport: func(error) { reports.Add(1) }}
				h := taskTestHarnessOptions(t, b.store, options, def)
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				for interruption := uint64(1); interruption <= 2; interruption++ {
					seedInterrupted140(t, h, id)
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
					h = taskTestHarnessOptions(t, reopenStoreAfterHarnessClose(t, h.session.store), options, def)
					state, err := h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					if task := state.Tasks[id]; task.Restarts != interruption || task.Status != "pending" {
						t.Fatalf("recovery: %+v", task)
					}
				}
				limit = 999 // Open must detach live host settings.
				if h.RestartLimit() != 2 || reports.Load() != 1 || runs.Load() != 0 {
					t.Fatal(h.RestartLimit(), reports.Load(), runs.Load())
				}
				inspect, err := h.InspectTasks(bg)
				if err != nil || len(inspect.Tasks) != 1 || inspect.Tasks[0].Reason != "restart_limit" || inspect.Tasks[0].Record.Restarts != 2 {
					t.Fatalf("%+v %v", inspect, err)
				}
				if err = h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				switch action {
				case "retry":
					if err = h.RetryTask(bg, id); err != nil {
						t.Fatal(err)
					}
				case "raise":
					cap := uint64(3)
					if err = h.SetSettings(bg, HarnessSettings{RestartLimit: &cap}); err != nil {
						t.Fatal(err)
					}
				case "abort":
					if _, err = h.AbortTask(bg, id); err != nil {
						t.Fatal(err)
					}
				}
				result := waitPublicTask(t, h, id)
				want := "completed"
				if action == "abort" {
					want = "aborted"
				}
				if result.State.Outcome.Status != want || result.Restarts != 0 {
					t.Fatalf("%+v", result)
				}
				expected := int64(1)
				if action == "abort" {
					expected = 0
				}
				if runs.Load() != expected {
					t.Fatal(runs.Load())
				}
				if err = h.RetryTask(bg, id); err != nil {
					t.Fatal("terminal retry must be no-op", err)
				}
				if err = h.RetryTask(bg, id+999); err == nil {
					t.Fatal("unknown task retry accepted")
				}
			})
		})
	}
}

func TestRestartLimit140ProgressNotIntentOrMemo(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		def := taskDefinition(t, "task.restart-progress-140", func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
			if record.Restarts != 1 {
				return fmt.Errorf("entry count %d", record.Restarts)
			}
			if err := r.Commit(ctx, func(_ *Tx, current TaskRecord) (*TaskState, error) {
				state := current.State
				state.Checkpoint["number"] = json.Number("1.0")
				return &state, nil
			}); err != nil {
				return err
			}
			if _, err := r.MemoCandidate(ctx, "memo", "value"); err != nil {
				return err
			}
			if err := r.Commit(ctx, func(_ *Tx, current TaskRecord) (*TaskState, error) {
				if current.Restarts != 1 {
					return nil, fmt.Errorf("identical intent/memo reset count: %d", current.Restarts)
				}
				return &TaskState{Status: "running", Checkpoint: JSON{"phase": "work", "progress": true}}, nil
			}); err != nil {
				return err
			}
			return r.Commit(ctx, func(_ *Tx, current TaskRecord) (*TaskState, error) {
				if current.Restarts != 0 {
					return nil, fmt.Errorf("progress retained count: %d", current.Restarts)
				}
				return taskDone(nil), nil
			})
		})
		h := taskTestHarness(t, b.store, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			task, err := copyTask(tx.state.Tasks[id], tx.limits)
			if err != nil {
				return err
			}
			task.Checkpoint = JSON{}
			task.Execution.Native.State.Checkpoint["number"] = json.Number("1")
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		seedInterrupted140(t, h, id)
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = taskTestHarness(t, reopenStoreAfterHarnessClose(t, h.session.store), def)
		if h.RestartLimit() != DefaultRestartLimit {
			t.Fatal(h.RestartLimit())
		}
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome == nil || record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v", record.State.Outcome)
		}
	})
}
