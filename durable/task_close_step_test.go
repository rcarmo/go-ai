package durable

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskSchedulerPinnedCloseAtReturnedFaultOrProgressStepWritesNoOutcome(t *testing.T) {
	for _, mode := range []string{"fault", "progress"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, release := make(chan struct{}), make(chan struct{})
				var captured *TaskRuntime
				var first, second, aborts atomic.Int64
				definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.close-step", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "one"}, nil }, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { aborts.Add(1); return nil }, Phases: map[string]TaskPhase{
					"one": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						first.Add(1)
						captured = r
						if mode == "progress" {
							if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
								return &TaskState{Status: "running", Checkpoint: JSON{"phase": "two"}}, nil
							}); err != nil {
								return err
							}
						}
						close(entered)
						<-release
						if mode == "fault" {
							return errors.New("actual returned phase error")
						}
						return nil
					},
					"two": func(context.Context, TaskRecord, *TaskRuntime) error { second.Add(1); return nil },
				}})
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, b.store, definition)
				cleanupTaskGates(t, release)
				id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				awaitTaskSignal(t, entered)
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var closing chan error
				_, err = h.session.taskCommit(bg, func(*Tx) error {
					// Hold the real Session line across actual handler return; the
					// scheduler's next step cannot decide or refresh under this hold.
					close(release)
					deadline, cancel := context.WithTimeout(bg, 3*time.Second)
					defer cancel()
					for {
						captured.phaseMu.Lock()
						draining := captured.phaseDraining
						captured.phaseMu.Unlock()
						if draining {
							break
						}
						select {
						case <-deadline.Done():
							return deadline.Err()
						default:
							runtime.Gosched()
						}
					}
					closing = make(chan error, 1)
					go func() { closing <- h.Close(bg) }()
					for !h.closing.Load() {
						select {
						case <-deadline.Done():
							return deadline.Err()
						default:
							runtime.Gosched()
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-closing:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("Close step join missing")
				}
				store := reopenStoreAfterHarnessClose(t, b.store)
				t.Cleanup(func() { _ = store.Close(bg) })
				after := snap(t, store)
				if before.Seq != after.Seq || after.Tasks[id].Status != "running" || taskHasDecidedOutcome(after.Tasks[id]) || first.Load() != 1 || second.Load() != 0 || aborts.Load() != 0 {
					t.Fatal("Close step published decision/fresh work", before.Seq, after.Seq, after.Tasks[id], first.Load(), second.Load(), aborts.Load())
				}
				if mode == "progress" && after.Tasks[id].Execution.Native.State.Checkpoint["phase"] != "two" {
					t.Fatal("committed progress lost", after.Tasks[id])
				}
			})
		})
	}
}
