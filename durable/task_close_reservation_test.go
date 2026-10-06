package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestTaskSchedulerPinnedCloseJoinsStorageSettlingRunAndAbortReservations(t *testing.T) {
	for _, mode := range []string{"run", "abort"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var runs, aborts atomic.Int64
				definition := taskDefinition(t, "task.close-reservation", func(context.Context, TaskRecord, *TaskRuntime) error { runs.Add(1); return nil })
				definition.options.Abort = func(context.Context, TaskRecord, *TaskRuntime) error { aborts.Add(1); return nil }
				h := taskTestHarness(t, b.store, definition)
				id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if mode == "abort" {
					_, err := h.session.taskCommit(bg, func(tx *Tx) error {
						marked := markTask(tx.state.Tasks[id])
						return tx.stage(Write{Op: "put-task", Task: &marked})
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				core := taskBackendCore(t, b.store)
				var original func(byte, uint64, uint64, []byte) error
				h.session.taskBookkeeping(func() { <-core.line; original = core.appendFrame; core.leave() })
				entered, release := make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, release)
				var once atomic.Bool
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var commit commitRecord
						if err := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &commit); err != nil {
							return err
						}
						for _, write := range commit.Writes {
							if write.Task != nil && write.Task.ID == id && write.Task.Status == "running" && once.CompareAndSwap(false, true) {
								close(entered)
								<-release
							}
						}
					}
					if original != nil {
						return original(kind, ordinal, high, payload)
					}
					return nil
				})
				type reservation struct {
					r   []*TaskRuntime
					err error
				}
				reserved := make(chan reservation, 1)
				go func() { r, err := h.scheduler.reservePass(false); reserved <- reservation{r, err} }()
				awaitTaskSignal(t, entered)
				closed := make(chan error, 1)
				go func() { closed <- h.Close(bg) }()
				<-h.life.Done()
				select {
				case err := <-closed:
					t.Fatal("close lost unsettled reservation", err)
				default:
				}
				close(release)
				admitted := <-reserved
				if admitted.err != nil || len(admitted.r) != 1 {
					t.Fatal(admitted)
				}
				go h.scheduler.run(admitted.r[0])
				if err := <-closed; err != nil {
					t.Fatal(err)
				}
				if runs.Load() != 0 || aborts.Load() != 0 {
					t.Fatal("handler started after seal", runs.Load(), aborts.Load())
				}
				second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), definition)
				state, err := second.Snapshot(bg)
				if err != nil || state.Tasks[id].Status != "pending" || taskHasDecidedOutcome(state.Tasks[id]) || taskAborted(state.Tasks[id]) != (mode == "abort") {
					t.Fatal("close invented decision", state.Tasks[id], err)
				}
			})
		})
	}
}

func taskBackendCore(t *testing.T, store Storage) *storeCore {
	t.Helper()
	switch s := store.(type) {
	case *MemoryStorage:
		return s.storeCore
	case *JournalStorage:
		return s.storeCore
	default:
		t.Fatal("test backend unavailable")
		return nil
	}
}

func TestTaskSchedulerPinnedCloseRejectsRuntimeCommitQueuedAtAdmission(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, releaseHost := make(chan struct{}), make(chan struct{})
		captured := make(chan *TaskRuntime, 1)
		definition := taskDefinition(t, "task.close-queued-commit", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured <- r
			close(entered)
			<-releaseHost
			return nil
		})
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, releaseHost)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		picked := <-captured
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		probe := &taskAdmissionContext{Context: bg, queued: make(chan struct{})}
		result, closed := make(chan error, 1), make(chan error, 1)
		_, err = h.session.taskCommit(bg, func(*Tx) error {
			go func() {
				result <- picked.Commit(probe, func(*Tx, TaskRecord) (*TaskState, error) {
					t.Error("post-seal runtime callback ran")
					return taskDone("forbidden"), nil
				})
			}()
			awaitTaskSignal(t, probe.queued)
			go func() { closed <- h.Close(bg) }()
			<-h.life.Done()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		err = <-result
		if !errors.Is(err, ErrClosed) && !errors.Is(err, ErrSealed) {
			t.Fatal("queued commit survived seal", err)
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Seq != before.Seq || taskHasDecidedOutcome(state.Tasks[id]) {
			t.Fatal("queued decision published", state.Seq, before.Seq, err)
		}
		select {
		case err := <-closed:
			t.Fatal("close abandoned actual handler", err)
		default:
		}
		close(releaseHost)
		if err := <-closed; err != nil {
			t.Fatal(err)
		}
	})
}
