package durable

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// A real running host keeps Wake active. Terminal history must not hide the
// sleeping deadline; setup is outside the benchmark's measured interval.
func mainWakeIndexFixture(tb testing.TB, history int) (*Harness, ID, ID) {
	tb.Helper()
	store, err := newTestSQLite(tb)
	if err != nil {
		tb.Fatal(err)
	}
	// Tasks live in the reserved root, which its owner creates.
	seedRootConversation(tb, store)
	ids := make([]ID, history+2)
	for i := range ids {
		ids[i], err = store.MintID(bg)
		if err != nil {
			tb.Fatal(err)
		}
	}
	writes := make([]Write, 0, history+2)
	for _, id := range ids[:history] {
		task := mainWakeSourceTask(id, *taskDone(nil))
		task.Status = "done"
		writes = append(writes, Write{Op: "put-task", Task: &task})
	}
	sleeper := mainWakeSourceSleep(ids[history], 5000)
	host := mainWakeSourceTask(ids[history+1], TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}})
	writes = append(writes, Write{Op: "put-task", Task: &sleeper}, Write{Op: "put-task", Task: &host})
	for len(writes) > 0 {
		n := min(len(writes), 128)
		if _, err := store.Apply(bg, Batch{Writes: writes[:n]}); err != nil {
			tb.Fatal(err)
		}
		writes = writes[n:]
	}
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	def, err := DefineTask(TaskDefinitionOptions{Kind: "task.wake-source", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil },
		Phases: map[string]TaskPhase{"work": func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return nil
		}}, Abort: func(context.Context, TaskRecord, *TaskRuntime) error { return nil },
	})
	if err != nil {
		tb.Fatal(err)
	}
	registry := NewRegistry()
	if _, err := registry.RegisterTask(def); err != nil {
		tb.Fatal(err)
	}
	h, err := Open(bg, store, Options{Registry: registry, Now: func() int64 { return 1000 }, OnWake: func(Wake) {}})
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		unblock()
		if err := h.Close(bg); err != nil {
			tb.Error(err)
		}
	})
	if err := h.Resume(bg); err != nil {
		tb.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		tb.Fatal("host did not start")
	}
	at := int64(5000)
	if wake, err := h.Wake(bg); err != nil || !sameWake(wake, Wake{Active: true, At: &at}) {
		tb.Fatal(wake, err)
	}
	return h, sleeper.ID, host.ID
}

func BenchmarkMainWakeActiveHistory(b *testing.B) {
	h, _, _ := mainWakeIndexFixture(b, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wake, err := h.Wake(bg)
		if err != nil || !wake.Active || wake.At == nil || *wake.At != 5000 {
			b.Fatal(wake, err)
		}
	}
}

func TestMainWakeSleepingIndexPublication(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		id, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainWakeSourceSeed(t, b.store, mainWakeSourceSleep(id, 5000))
		def := taskDefinition(t, "task.wake-source", func(context.Context, TaskRecord, *TaskRuntime) error {
			t.Error("paused fixture invoked host")
			return nil
		})
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1000 }, OnWake: func(Wake) {}}, def)
		check := func(wantAt int64) {
			t.Helper()
			if err := h.session.readTasks(bg, func(state Snapshot) error {
				if h.scheduler.wakeSleeping == nil || h.scheduler.wakeSleepingSeq != state.Seq {
					t.Fatal("publication invalidated or missed incremental index", h.scheduler.wakeSleepingSeq, state.Seq)
				}
				count := 0
				for id, task := range state.Tasks {
					if task.Status == "sleeping" {
						count++
						cached, ok := h.scheduler.wakeSleeping[id]
						if !ok || !reflect.DeepEqual(cached, task) {
							t.Fatal("cached task differs from final adopted task", id)
						}
					}
				}
				if len(h.scheduler.wakeSleeping) != count {
					t.Fatal("index retained a non-sleeper")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			wake, err := h.Wake(bg)
			if err != nil || wake.Active || wantAt == 0 && wake.At != nil || wantAt != 0 && (wake.At == nil || *wake.At != wantAt) {
				t.Fatal(wake, wantAt, err)
			}
		}
		check(5000)
		// Internal stage deliberately isolates publication bookkeeping. Public raw
		// replacement of managed tasks remains forbidden; no authority is granted.
		if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
			task, err := copyTask(tx.state.Tasks[id], tx.limits)
			if err != nil {
				return nil, err
			}
			for _, until := range []int64{6000, 7000} {
				task.Execution.Native.State.Until = &until
				if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		check(7000)
		entry, err := h.session.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "wake-index", Value: JSON{}})
		}); err != nil {
			t.Fatal(err)
		}
		check(7000)
		if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
			task, err := copyTask(tx.state.Tasks[id], tx.limits)
			if err != nil {
				return nil, err
			}
			task.Status = "done"
			task.Execution.Native.State = *taskDone(nil)
			return nil, tx.stage(Write{Op: "put-task", Task: &task})
		}); err != nil {
			t.Fatal(err)
		}
		check(0)
		// A private adopted receipt bypasses scheduler effects. The read fallback
		// must rebuild using its own published image, never a later raw core image.
		next, err := h.session.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			task := mainWakeSourceSleep(next, 9000)
			var receipt commitReceipt
			if _, err := h.session.core.commitParts(bg, []Batch{{Writes: []Write{{Op: "put-task", Task: &task}}}}, func(r commitReceipt) error { receipt = r; return nil }, true); err != nil {
				t.Fatal(err)
			}
			if err := h.session.adoptReceipt(receipt, nil); err != nil {
				t.Fatal(err)
			}
			if h.scheduler.wakeSleepingSeq == h.session.readImage.Seq {
				t.Fatal("fixture did not create stale index")
			}
			h.scheduler.sleepingForWake(h.session.readImage)
		})
		check(9000)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		if h.scheduler.wakeSleeping != nil {
			t.Fatal("Close retained sleeping index")
		}
	})
}

type mainWakeIndexAdapter struct {
	nativeStorage
	sleeper ID
	failure error
}

func (a *mainWakeIndexAdapter) Snapshot(ctx context.Context) (Snapshot, error) {
	if a.failure != nil {
		return Snapshot{}, a.failure
	}
	state, err := a.nativeStorage.Snapshot(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	task := state.Tasks[a.sleeper]
	until := int64(7000)
	task.Execution.Native.State.Until = &until
	state.Tasks[a.sleeper] = task
	return state, nil
}
func TestMainWakeSleepingIndexPreservesActiveWrapper(t *testing.T) {
	h, sleeper, _ := mainWakeIndexFixture(t, 8)
	native := h.session.store
	adapter := &mainWakeIndexAdapter{nativeStorage: native.(nativeStorage), sleeper: sleeper}
	h.session.taskBookkeeping(func() { h.session.store = adapter })
	defer h.session.taskBookkeeping(func() { h.session.store = native })
	at := int64(7000)
	if wake, err := h.Wake(bg); err != nil || !sameWake(wake, Wake{Active: true, At: &at}) {
		t.Fatal("adapter data bypassed", wake, err)
	}
	failure := storageRequest("wake wrapper gate")
	h.session.taskBookkeeping(func() { adapter.failure = failure })
	if _, err := h.Wake(bg); !errors.Is(err, failure) {
		t.Fatal("adapter error bypassed", err)
	}
}

func TestMainWakeDueAlarmDoesNotReplaceFutureDeadline(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		first, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		second, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainWakeSourceSeed(t, b.store, mainWakeSourceSleep(first, 500), mainWakeSourceSleep(second, 5000))
		def := taskDefinition(t, "task.wake-source", func(context.Context, TaskRecord, *TaskRuntime) error {
			t.Error("paused fixture invoked host")
			return nil
		})
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1000 }, OnWake: func(Wake) {}}, def)
		for i := 0; i < 20; i++ {
			details, err := h.readWake(bg)
			if err != nil || !details.wake.Active || details.wake.At == nil || *details.wake.At != 5000 || details.alarmAt == nil || *details.alarmAt != 500 {
				t.Fatal(details, err)
			}
		}
	})
}
