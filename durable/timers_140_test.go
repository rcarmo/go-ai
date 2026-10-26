package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type capturedTimer140 struct {
	fire      func()
	cancelled atomic.Int64
}

func TestTimer140InvocationScopeAndOffLineHost(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var fired atomic.Int64
		captured := make(chan *capturedTimer140, 4)
		hosts := make(chan *TaskRuntime, 1)
		var h *Harness
		options := Options{SetTimer: func(callback func(), _ time.Duration, _ TimerOptions) func() {
			if _, err := h.TaskGraph(bg); err != nil {
				t.Error("timer host reentrancy", err)
			}
			timer := &capturedTimer140{fire: callback}
			captured <- timer
			return func() {
				if _, err := h.TaskGraph(bg); err != nil && err != ErrClosed {
					t.Error("timer cancellation reentrancy", err)
				}
				timer.cancelled.Add(1)
			}
		}}
		def := taskDefinition(t, "task.scoped-timer-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			hosts <- r
			cancel, err := r.SetTimer(func() { fired.Add(1) }, time.Hour)
			if err != nil {
				return err
			}
			cancel()
			cancel()
			if _, err = r.SetTimer(func() { fired.Add(1) }, time.Hour); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h = taskTestHarnessOptions(t, b.store, options, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err := h.WaitForTask(ctx, id); err != nil {
			t.Fatal(err)
		}
		runtime := <-hosts
		awaitTaskSignal(t, runtime.done)
		first, second := <-captured, <-captured
		if first.cancelled.Load() != 1 || second.cancelled.Load() != 1 {
			t.Fatal("timer cancellation not exactly once", first.cancelled.Load(), second.cancelled.Load())
		}
		first.fire()
		second.fire()
		if fired.Load() != 0 {
			t.Fatal("ended invocation timer fired", fired.Load())
		}
		if _, err := runtime.SetTimer(func() {}, time.Hour); err != ErrSealed {
			t.Fatal("ended scope admitted timer", err)
		}
	})
}
func TestTimer140DeclinedForegroundFallbackAndBackground(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var reports, fired atomic.Int64
		options := Options{SetTimer: func(func(), time.Duration, TimerOptions) func() { return nil }, OnReport: func(error) { reports.Add(1) }}
		def := taskDefinition(t, "task.timer-fallback-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.SetTimer(func() { fired.Add(100) }, 0, TimerOptions{Background: true}); err != nil {
				return err
			}
			for n := 0; n < 2; n++ {
				gate := make(chan struct{})
				if _, err := r.SetTimer(func() { fired.Add(1); close(gate) }, time.Millisecond); err != nil {
					return err
				}
				select {
				case <-gate:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, options, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err := h.WaitForTask(ctx, id); err != nil {
			t.Fatal(err)
		}
		if fired.Load() != 2 || reports.Load() != 1 {
			t.Fatal("fallback/decline behavior", fired.Load(), reports.Load())
		}
	})
}
