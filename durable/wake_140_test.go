package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func awaitWake140(t *testing.T, events <-chan Wake, active bool, at *int64) Wake {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case value := <-events:
			if sameWake(value, Wake{Active: active, At: at}) {
				return value
			}
		case <-timer.C:
			t.Fatalf("wake never reached active=%v at=%v", active, at)
		}
	}
}
func TestWake140PausedObservationCallbacksAndNoSleepTimer(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock, after, timers atomic.Int64
		clock.Store(1000)
		hosts := make(chan *TaskRuntime, 1)
		events := make(chan Wake, 128)
		var current atomic.Pointer[Harness]
		var activeOffered atomic.Bool
		options := Options{Now: clock.Load, SetTimer: func(func(), time.Duration, TimerOptions) func() { timers.Add(1); return func() {} }, OnWake: func(w Wake) {
			if h := current.Load(); h != nil {
				if _, err := h.Wake(bg); err != nil {
					t.Error("wake callback read reentrancy", err)
				}
			}
			if w.Active {
				activeOffered.Store(true)
			}
			events <- copyWake(w)
			if w.At != nil {
				*w.At = 1
			} // mutation must not corrupt subsequent comparisons
		}}
		def := sleepDefinition140(t, 2000, &after, hosts, nil)
		h := taskTestHarnessOptions(t, b.store, options, def)
		current.Store(h)
		awaitWake140(t, events, false, nil)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		awaitWake140(t, events, true, nil)
		if !activeOffered.Load() {
			t.Fatal("activity not offered before scheduling")
		}
		select {
		case <-hosts:
			t.Fatal("paused wake observation dispatched task")
		default:
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		until := int64(2000)
		awaitWake140(t, events, false, &until)
		value, err := h.Wake(bg)
		if err != nil || !sameWake(value, Wake{At: &until}) {
			t.Fatal(value, err)
		}
		if timers.Load() != 0 {
			t.Fatal("onWake armed sleep timers", timers.Load())
		}
		clock.Store(1999)
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		if after.Load() != 0 {
			t.Fatal("early wake ran continuation")
		}
		clock.Store(2000)
		value, err = h.Wake(bg)
		if err != nil || !value.Active || value.At != nil {
			t.Fatal("due sleep not active", value, err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err = h.WaitForTask(ctx, id); err != nil {
			t.Fatal(err)
		}
		awaitWake140(t, events, false, nil)
		if after.Load() != 1 || timers.Load() != 0 {
			t.Fatal(after.Load(), timers.Load())
		}
	})
}
func TestWake140FailedDeliveryReofferedAndResumeReentrancy(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock, after atomic.Int64
		clock.Store(1000)
		var current atomic.Pointer[Harness]
		var failing atomic.Bool
		failing.Store(true)
		offered, delivered := make(chan Wake, 64), make(chan Wake, 64)
		reported := make(chan struct{}, 64)
		hosts := make(chan *TaskRuntime, 1)
		options := Options{Now: clock.Load, OnReport: func(error) { reported <- struct{}{} }, OnWake: func(w Wake) {
			if w.At != nil && failing.Load() {
				offered <- copyWake(w)
				panic("alarm unavailable")
			}
			delivered <- copyWake(w)
			if w.Active {
				if h := current.Load(); h != nil {
					if err := h.Resume(bg); err != nil {
						t.Error(err)
					}
				}
			}
		}}
		def := sleepDefinition140(t, 2000, &after, hosts, nil)
		h := taskTestHarnessOptions(t, b.store, options, def)
		current.Store(h)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		// The host callback, not this test, resumes the newly committed work.
		until := int64(2000)
		awaitWake140(t, offered, false, &until)
		awaitTaskSignal(t, reported)
		failing.Store(false)
		if err := h.options.Registry.Install(&Extension{Name: "wake.reoffer"}); err != nil {
			t.Fatal(err)
		}
		awaitWake140(t, delivered, false, &until)
		clock.Store(2000)
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err := h.WaitForTask(ctx, id); err != nil {
			t.Fatal(err)
		}
	})
}
func TestWake140AlarmDedupEarlyFireAndClose(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock, after atomic.Int64
		clock.Store(1000)
		hosts := make(chan *TaskRuntime, 1)
		armed := make(chan *capturedTimer140, 32)
		var current atomic.Pointer[Harness]
		options := Options{Now: clock.Load, SetTimer: func(callback func(), delay time.Duration, mode TimerOptions) func() {
			if mode.Background {
				return nil
			}
			if delay != time.Second {
				t.Errorf("unexpected sleep delay %v", delay)
			}
			timer := &capturedTimer140{fire: callback}
			armed <- timer
			return func() {
				timer.cancelled.Add(1)
				if h := current.Load(); h != nil {
					_ = h.Resume(bg)
				}
			}
		}}
		def := sleepDefinition140(t, 2000, &after, hosts, nil)
		h := taskTestHarnessOptions(t, b.store, options, def)
		current.Store(h)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitSleeping140(t, h, id)
		awaitTaskSignal(t, (<-hosts).done)
		first := <-armed
		// An unrelated reservation publishes wake metadata before its handler runs.
		// It must reuse the already armed deadline, not cancel/rearm it.
		other := taskDefinition(t, "task.wake-alarm-barrier", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := h.options.Registry.RegisterTask(other); err != nil {
			t.Fatal(err)
		}
		otherID := createPublicTask(t, h, other, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err := h.WaitForTask(ctx, otherID); err != nil {
			t.Fatal(err)
		}
		if first.cancelled.Load() != 0 {
			t.Fatal("unchanged deadline cancelled")
		}
		select {
		case <-armed:
			t.Fatal("unchanged deadline rearmed")
		default:
		}
		first.fire() // early: recheck and rearm for the still-future deadline
		var second *capturedTimer140
		select {
		case second = <-armed:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		if first.cancelled.Load() != 1 {
			t.Fatal("fired alarm not disarmed")
		}
		first.fire() // stale cancellation/firing cannot invalidate the new alarm
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		if second.cancelled.Load() != 1 {
			t.Fatal("close did not disarm sleep alarm")
		}
		second.fire()
		if after.Load() != 0 {
			t.Fatal("late alarm ran continuation")
		}
	})
}
func TestWake140WaitsAndHeldOutcomesFollowSleepingWork(t *testing.T) {
	for _, hold := range []bool{false, true} {
		backends(t, func(t *testing.T, b backend) {
			var after atomic.Int64
			events := make(chan Wake, 128)
			hosts := make(chan *TaskRuntime, 1)
			child := sleepDefinition140(t, 2000, &after, hosts, nil)
			parent := taskDefinition(t, "task.wake-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					id, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
					if err != nil {
						return nil, err
					}
					if hold {
						return &TaskState{Status: "running", Checkpoint: JSON{"phase": "work", "child": id}}, nil
					}
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{id}, Policy: "allSettled"}, nil
				})
				if err != nil || !hold {
					return err
				}
				// New work is admitted while the owner is live, then a later
				// decision holds over it. Preserve the final-owner admission fence.
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1000 }, OnWake: func(w Wake) { events <- copyWake(w) }}, parent, child)
			createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			until := int64(2000)
			awaitWake140(t, events, false, &until)
			value, err := h.Wake(bg)
			if err != nil || value.Active || value.At == nil || *value.At != until {
				t.Fatal(value, err, hold)
			}
		})
	}
}

func TestWake140UnavailableDefinitionIsInactive(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var after atomic.Int64
		hosts := make(chan *TaskRuntime, 1)
		def := sleepDefinition140(t, 2000, &after, hosts, nil)
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1000 }})
		createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		value, err := h.Wake(bg)
		if err != nil || value.Active || value.At != nil {
			t.Fatal("missing definition counted as active", value, err)
		}
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		if err = h.session.readTasks(bg, func(Snapshot) error {
			if len(h.scheduler.invocations) != 0 {
				t.Error("wake observation dispatched unavailable definition")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if after.Load() != 0 {
			t.Fatal("unavailable task ran")
		}
	})
}
