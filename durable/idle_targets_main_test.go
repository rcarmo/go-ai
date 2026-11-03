package durable

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"
)

func TestMainIdleTargetsSkipHistory(t *testing.T) {
	h, sleeper, host := mainWakeIndexFixture(t, 4096)
	want := []ID{sleeper, host}
	slices.Sort(want)
	err := h.session.readTaskView(bg, func(state Snapshot, published bool) error {
		if !published {
			t.Fatal("native read lost its witness")
		}
		x := h.scheduler.currentIdleIndex()
		for _, scope := range []ID{0, 1} {
			before := x.targetVisits
			got := h.scheduler.idleTargetsForView(state, scope, published)
			visits := uint64(3)
			if scope == 0 {
				visits = 4
			}
			if !slices.Equal(got, want) || x.targetVisits-before != visits {
				t.Fatalf("scope%d targets=%v want=%v visits=%d want=%d", scope, got, want, x.targetVisits-before, visits)
			}
			got[0] = 0
			again, ok := h.scheduler.indexedIdleTargets(scope)
			if !ok || !slices.Equal(again, want) {
				t.Fatal("caller mutated target storage", again)
			}
		}
		// Same sequence, different candidate records. The witness belongs to the
		// source of the read, not the sequence number or allocator high-water mark.
		candidate := state
		candidate.Tasks = maps.Clone(state.Tasks)
		delete(candidate.Tasks, sleeper)
		got := h.scheduler.idleTargetsForView(candidate, 0, false)
		if !slices.Equal(got, []ID{host}) {
			t.Fatal("candidate used published targets", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type mainIdleTargetsAdapter struct {
	nativeStorage
	empty   bool
	failure error
}

func (a *mainIdleTargetsAdapter) Snapshot(ctx context.Context) (Snapshot, error) {
	if a.failure != nil {
		return Snapshot{}, a.failure
	}
	state, err := a.nativeStorage.Snapshot(ctx)
	if err == nil && a.empty {
		state.Tasks = map[ID]Task{}
	}
	return state, err
}
func TestMainIdleTargetsWrapperWitness(t *testing.T) {
	h, _, _ := mainWakeIndexFixture(t, 8)
	h.session.taskBookkeeping(func() { h.scheduler.currentIdleIndex() })
	native := h.session.store
	a := &mainIdleTargetsAdapter{nativeStorage: native.(nativeStorage), empty: true}
	h.session.taskBookkeeping(func() { h.session.store = a })
	defer h.session.taskBookkeeping(func() { h.session.store = native })
	if err := h.session.readTaskView(bg, func(state Snapshot, published bool) error {
		if published || state.Seq != h.session.readImage.Seq {
			t.Fatal("adapter got native witness or fixture lost equal sequence", published)
		}
		if got := h.scheduler.idleTargetsForView(state, 0, published); len(got) != 0 {
			t.Fatal("adapter data bypassed", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, time.Second)
	defer cancel()
	if err := h.WaitForIdle(ctx); err != nil {
		t.Fatal("public idle wait bypassed adapter", err)
	}
	failure := storageRequest("idle target adapter failure")
	h.session.taskBookkeeping(func() { a.failure = failure })
	if err := h.WaitForIdle(ctx); !errors.Is(err, failure) {
		t.Fatal("public idle wait bypassed adapter error", err)
	}
}

func BenchmarkMainIdleTargetsHistory(b *testing.B) {
	h, sleeper, host := mainWakeIndexFixture(b, 4096)
	want := []ID{sleeper, host}
	slices.Sort(want)
	if err := h.session.readTaskView(bg, func(state Snapshot, published bool) error {
		h.scheduler.currentIdleIndex()
		b.ReportAllocs()
		for b.Loop() {
			got := h.scheduler.idleTargetsForView(state, 0, published)
			if !slices.Equal(got, want) {
				b.Fatal(got, want)
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
}

func TestMainIdleTargetsWideAndDeep(t *testing.T) {
	for _, chain := range []bool{false, true} {
		name := "wide"
		if chain {
			name = "chain"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				tasks := make([]Task, 64)
				want := []ID{}
				for i := range tasks {
					id, err := b.store.MintID(bg)
					if err != nil {
						t.Fatal(err)
					}
					task := mainWakeSourceSleep(id, 5000)
					if chain {
						if i > 0 {
							task.Owner = tasks[i-1].ID
						}
						if i < len(tasks)-1 {
							task.Status = "done"
							task.Execution.Native.State = *taskDone(nil)
						} else {
							want = append(want, id)
						}
					} else {
						want = append(want, id)
					}
					tasks[i] = task
				}
				mainWakeSourceSeed(t, b.store, tasks...)
				def := taskDefinition(t, "task.wake-source", func(context.Context, TaskRecord, *TaskRuntime) error {
					t.Error("paused enumeration invoked phase")
					return nil
				})
				h := taskTestHarness(t, b.store, def)
				h.session.taskBookkeeping(func() {
					x := h.scheduler.currentIdleIndex()
					before := x.targetVisits
					got, ok := h.scheduler.indexedIdleTargets(0)
					if !ok || !slices.Equal(got, want) || x.targetVisits-before != 66 {
						t.Fatalf("targets=%v want=%v visits=%d", got, want, x.targetVisits-before)
					}
				})
			})
		})
	}
}
