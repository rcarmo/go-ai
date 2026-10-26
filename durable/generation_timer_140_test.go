package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestGenerationTimer140ProductionClockEarlyFireAndTerminalDisarm(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		more, release := make(chan struct{}), make(chan struct{})
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event)
			go func() {
				defer close(events)
				select {
				case events <- &goai.TextDeltaEvent{Partial: terminal("first prefix").Message}:
				case <-ctx.Done():
					return
				}
				select {
				case <-more:
				case <-ctx.Done():
					return
				}
				select {
				case events <- &goai.TextDeltaEvent{Partial: terminal("second prefix").Message}:
				case <-ctx.Done():
					return
				}
				select {
				case <-release:
				case <-ctx.Done():
					return
				}
				select {
				case events <- terminal("answer"):
				case <-ctx.Done():
				}
			}()
			return events
		})
		var clock atomic.Int64
		clock.Store(1000)
		options.Now = clock.Load
		type alarm struct {
			timer *capturedTimer140
			delay time.Duration
		}
		armed := make(chan alarm, 16)
		options.SetTimer = func(callback func(), delay time.Duration, mode TimerOptions) func() {
			if mode.Background {
				return nil
			}
			node := &capturedTimer140{fire: callback}
			armed <- alarm{node, delay}
			return func() { node.cancelled.Add(1) }
		}
		h := openHarness(t, b.store, options)
		t.Cleanup(func() { releaseTaskGate(more); releaseTaskGate(release) })
		r := root(t, h, ref)
		submitted, err := r.Submit(bg, Input{Content: "input", RequestID: "partial-host-timer"})
		if err != nil {
			t.Fatal(err)
		}
		next := func() alarm {
			t.Helper()
			select {
			case a := <-armed:
				return a
			case <-time.After(3 * time.Second):
				t.Fatal("partial host timer absent")
				return alarm{}
			}
		}
		first := next()
		if first.delay != 100*time.Millisecond {
			t.Fatal(first.delay)
		}
		first.timer.fire()
		second := next()
		if second.delay != 100*time.Millisecond {
			t.Fatal("early timer did not retain same clock deadline", second.delay)
		}
		var taskID ID
		partial := func() *MessageReceipt {
			t.Helper()
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			for _, task := range state.Tasks {
				if task.Kind != "pi.generation" {
					continue
				}
				var cp generationCheckpoint
				if err := fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if cp.Submission == submitted.ID() {
					taskID = task.ID
					return cp.Partial
				}
			}
			return nil
		}
		if value := partial(); value != nil {
			t.Fatal("early timer committed partial")
		}
		clock.Store(1100)
		second.timer.fire()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			if value := partial(); value != nil {
				if len(value.Content) != 1 || value.Content[0].Text != "first prefix" {
					t.Fatal(value)
				}
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
		releaseTaskGate(more)
		third := next()
		if third.delay != 100*time.Millisecond {
			t.Fatal(third.delay)
		}
		// Terminal drain must cancel this trailing partial timer and join its actor
		// before committing the final result, without advancing the fake clock.
		releaseTaskGate(release)
		if result := waitSubmission(t, submitted); result.Submission.Status != "done" {
			t.Fatal(result.Submission.Status)
		}
		if _, err := h.WaitForTask(ctx, taskID); err != nil {
			t.Fatal(err)
		}
		if third.timer.cancelled.Load() != 1 {
			t.Fatal("terminal drain retained timer", third.timer.cancelled.Load())
		}
		third.timer.fire()
		if value := partial(); value != nil && len(value.Content) > 0 && value.Content[0].Text == "second prefix" {
			t.Fatal("late timer published dropped window")
		}
	})
}
