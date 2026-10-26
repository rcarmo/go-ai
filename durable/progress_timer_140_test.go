package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestProgress140RuntimeClockEarlyTimersAndStop(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock, writes atomic.Int64
		clock.Store(1000)
		type alarm struct {
			timer *capturedTimer140
			delay time.Duration
		}
		armed := make(chan alarm, 16)
		stop := make(chan struct{})
		ready := make(chan struct{}, 1)
		nextAlarm := func() alarm {
			t.Helper()
			select {
			case a := <-armed:
				return a
			case <-time.After(3 * time.Second):
				t.Fatal("progress timer missing")
				return alarm{}
			}
		}
		var stopOnce atomic.Bool
		release := func() {
			if stopOnce.CompareAndSwap(false, true) {
				close(stop)
			}
		}
		// Release before the Harness cleanup tries to join the actual host.
		defer release()
		def := taskDefinition(t, "task.progress-clock-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			progress := newRuntimeToolProgress(func() (int, error) { writes.Add(1); return 0, nil }, nil, r, 100*time.Millisecond)
			defer func() { settleProgress(progress.stop(), nil) }()
			if err := <-progress.mark(true); err != nil {
				return err
			}
			if err := <-progress.mark(true); err != nil {
				return err
			}
			pending := progress.mark(true)
			ready <- struct{}{}
			<-stop
			settleProgress(progress.stop(), nil)
			if err := <-pending; err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Now: clock.Load, SetTimer: func(callback func(), delay time.Duration, mode TimerOptions) func() {
			if mode.Background {
				return nil
			}
			timer := &capturedTimer140{fire: callback}
			armed <- alarm{timer, delay}
			return func() { timer.cancelled.Add(1) }
		}}, def)
		t.Cleanup(release)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		first := nextAlarm()
		if first.delay != 100*time.Millisecond {
			t.Fatal(first.delay)
		}
		first.timer.fire() // timer without elapsed clock cannot release progress
		second := nextAlarm()
		if second.delay != 100*time.Millisecond || writes.Load() != 1 {
			t.Fatal(second.delay, writes.Load())
		}
		clock.Store(1099)
		second.timer.fire()
		third := nextAlarm()
		if third.delay != time.Millisecond || writes.Load() != 1 {
			t.Fatal(third.delay, writes.Load())
		}
		clock.Store(1100)
		third.timer.fire()
		awaitTaskSignal(t, ready)
		fourth := nextAlarm()
		if fourth.delay != 100*time.Millisecond {
			t.Fatal(fourth.delay)
		}
		release()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err := h.WaitForTask(ctx, id); err != nil {
			t.Fatal(err)
		}
		if fourth.timer.cancelled.Load() != 1 {
			t.Fatal("pending progress timer not disarmed", fourth.timer.cancelled.Load())
		}
		fourth.timer.fire()
		if writes.Load() != 2 {
			t.Fatal("late timer flushed pending final progress", writes.Load())
		}
	})
}

func TestProgress140ArmFailureSettlesAndReports(t *testing.T) {
	failure := errors.New("timer admission rejected")
	reported := make(chan error, 1)
	var writes atomic.Int64
	p := newToolProgressTimer(func() (int, error) { writes.Add(1); return 0, nil }, func(err error) { reported <- err }, func() time.Time { return time.UnixMilli(1000) }, func(time.Duration) (<-chan time.Time, func(), error) { return nil, nil, failure }, 100*time.Millisecond)
	defer p.stop()
	if err := <-p.mark(true); err != nil {
		t.Fatal(err)
	}
	waiter := p.mark(true)
	select {
	case err := <-waiter:
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("arm failure stranded waiter")
	}
	select {
	case err := <-reported:
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("arm failure unreported")
	}
	if writes.Load() != 1 {
		t.Fatal("failed arm flushed early", writes.Load())
	}
}
