package durable

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Official v1.7.0 harness-blocked.test.ts recovery-at-open contract. The
// storage rejection is a real SQLite append failure; the host wake is the
// public process-liveness observation, not a test-only scheduler hint.
func TestHarnessBlockedRecoveryWakeSurvivesRejectedCommit(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		definition := taskDefinition(t, "task.blocked-recovery-v170", func(context.Context, TaskRecord, *TaskRuntime) error {
			return errors.New("must not run during recovery")
		})
		first := taskTestHarness(t, b.store, definition)
		id := createPublicTask(t, first, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := interruptedStore140(t, reopenStoreAfterHarnessClose(t, b.store), id)
		core := store.(*SQLiteStorage).storeCore
		primary := errors.New("recovery reset")
		<-core.line
		appendFrame := core.appendFrame
		failed := false
		core.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 && !failed {
				failed = true
				return primary
			}
			if appendFrame != nil {
				return appendFrame(kind, ordinal, high, payload)
			}
			return nil
		}
		core.leave()

		var mu sync.Mutex
		var wakes []Wake
		h, err := Open(bg, store, Options{
			OnWake: func(wake Wake) {
				mu.Lock()
				wakes = append(wakes, copyWake(wake))
				mu.Unlock()
			},
		})
		if h != nil {
			_ = h.Close(bg)
			t.Fatal("failed recovery retained a Harness")
		}
		if !errors.Is(err, primary) {
			t.Fatal("recovery error replaced", err)
		}
		mu.Lock()
		got := append([]Wake(nil), wakes...)
		mu.Unlock()
		if !reflect.DeepEqual(got, []Wake{{Active: true}}) {
			t.Fatalf("failed recovery wakes = %+v, want active host wake only", got)
		}
	})
}

// Official v1.7.0 harness-blocked.test.ts missing-definition contract through
// the public registry, inspection and wake surfaces on SQLite.
func TestHarnessBlockedRegistryChangeUnblocksWake(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var runs atomic.Int64
		definition := taskDefinition(t, "task.blocked-registry-v170", func(ctx context.Context, _ TaskRecord, runtime *TaskRuntime) error {
			runs.Add(1)
			return runtime.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		wakes := make(chan Wake, 16)
		registry := NewRegistry()
		h := openHarness(t, b.store, Options{Registry: registry, OnWake: func(wake Wake) { wakes <- copyWake(wake) }})
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		inspection, err := h.Inspect(bg)
		if err != nil || len(inspection.Tasks) != 1 || inspection.Tasks[0].Kind != "blocked" || inspection.Tasks[0].Reason != "missing_task" {
			t.Fatal("missing definition not blocked", inspection, err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.RegisterTask(definition); err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case wake := <-wakes:
				if wake.Active {
					goto unblocked
				}
			case <-deadline.C:
				t.Fatal("registry change did not wake host")
			}
		}
	unblocked:
		result := waitPublicTask(t, h, id)
		if result.State.Outcome == nil || result.State.Outcome.Status != "completed" || runs.Load() != 1 {
			t.Fatal("registered definition did not run exactly once", result, runs.Load())
		}
	})
}
