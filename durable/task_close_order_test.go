package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskSchedulerPinnedCloseSealsAdmissionStopsWatchBeforeSignalAndJoins(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		notes, err := DefineDocument(DefinitionOptions{Kind: "app.close-order.notes", Scope: "conversation", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"text": "kept"}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		probed := make(chan error, 1)
		var phases, aborts atomic.Int64
		var h *Harness
		definition := taskDefinition(t, "task.close-order", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			phases.Add(1)
			watch, err := r.WatchDefinition(ctx, notes, 1, nil)
			if err != nil || watch == nil {
				return errors.New("close-order watch absent")
			}
			close(entered)
			<-ctx.Done()
			select {
			case <-watch.Closed():
				if end, ok := watch.End(); !ok || end.Reason != "stopped" {
					err = errors.New("close watch wrong end reason")
				}
			default:
				err = errors.New("handler cancellation preceded watch stop")
			}
			if _, rejected := h.CommitTasks(bg, 1, func(*Tx) error { return errors.New("late callback ran") }); !errors.Is(rejected, ErrClosed) {
				err = errors.New("close admission not sealed before signal")
			}
			probed <- err
			<-release
			return nil
		})
		definition.options.Abort = func(context.Context, TaskRecord, *TaskRuntime) error { aborts.Add(1); return nil }
		h = taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, release)
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error { _, err := tx.AcquireDocument(notes, 1, nil, nil); return err })
		if err != nil {
			t.Fatal(err)
		}
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		closing := make(chan error, 1)
		go func() { closing <- h.Close(bg) }()
		select {
		case err := <-probed:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("close cancellation probe missing")
		}
		select {
		case err := <-closing:
			t.Fatal("Close skipped actual host join", err)
		default:
		}
		close(release)
		if err := <-closing; err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		t.Cleanup(func() { _ = store.Close(bg) })
		after := snap(t, store)
		if after.Seq != before.Seq || after.Tasks[id].Status != "running" || taskHasDecidedOutcome(after.Tasks[id]) || phases.Load() != 1 || aborts.Load() != 0 {
			t.Fatal("Close wrote outcome or started fresh work", after.Tasks[id], before.Seq, after.Seq, phases.Load(), aborts.Load())
		}
	})
}
