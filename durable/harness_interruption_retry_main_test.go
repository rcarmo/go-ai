package durable

import (
	"context"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Sleep is not native WaitForIdle: a submission can remain unanswered while
// its host has yielded to a future wake. Sample the actual public wake instead.
func mainInterruptionQuiescent(ctx context.Context, h *Harness) error {
	for {
		wake, err := h.Wake(ctx)
		if err != nil {
			return err
		}
		if !wake.Active {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		runtime.Gosched()
	}
}

// Official f9a26365 harness-restart-failure.test.ts:180-202,242-273.
// Real failed-Session/reopen replaces process killing; the scripted stream
// acknowledges a stored prefix. No synthetic task state or retry is injected.
func TestMainHarnessInterruptedRequestRetryPolicy(t *testing.T) {
	for _, mode := range []string{"backoff", "disabled", "clean-close"} {
		t.Run(mode, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				var calls atomic.Int64
				first := &mainRecoveryStream{events: make(chan goai.Event, 1)}
				ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					if calls.Add(1) == 1 {
						go func() { <-ctx.Done(); first.close() }()
						return first.events
					}
					events := make(chan goai.Event, 1)
					events <- terminal("answer")
					close(events)
					return events
				})
				enabled, maximum, delay := true, 2, int64(60000)
				policy := &RetrySettings{Enabled: &enabled, MaxRetries: &maximum, BaseDelayMs: &delay}
				var initial *RetrySettings
				if mode == "backoff" {
					initial = policy
				} else if mode == "clean-close" {
					enabled = false
					initial = &RetrySettings{Enabled: &enabled}
				}
				var clock atomic.Int64
				clock.Store(1000)
				f := mainOpenRecoveryWithRetry(t, b.store, ref, options, &clock, 0, initial)
				sub, err := f.c.Submit(bg, Input{Content: "hi"})
				if err != nil {
					t.Fatal(err)
				}
				f.partial(first, strings.Repeat("z", 64))
				// Source stored() lets the zero-window alarm reach storage. Step
				// the native captured timer, then enter the real foreground gate.
				now := clock.Add(100)
				var jobs []func()
				f.timerMu.Lock()
				for _, timer := range f.timers {
					if !timer.cancelled && timer.due <= now {
						timer.cancelled = true
						jobs = append(jobs, timer.fire)
					}
				}
				f.timerMu.Unlock()
				for _, fire := range jobs {
					fire()
				}
				if _, err := f.h.Snapshot(bg); err != nil {
					t.Fatal(err)
				}
				if text, ok := mainRecoveryText(f.live(f.stored())); !ok || text != strings.Repeat("z", 64) {
					t.Fatal("fixture did not store its streamed prefix", text)
				}
				task, _, ok, err := f.h.nextTask(f.c.ID())
				if err != nil || !ok {
					t.Fatal("fixture generation missing", err)
				}
				var store Storage
				if mode == "clean-close" {
					if err := f.h.Close(bg); err != nil {
						t.Fatal(err)
					}
					store = utf16ReopenStore(t, b.store)
				} else {
					store = f.crash()
				}
				clock.Store(1000000)
				if mode != "backoff" {
					enabled = false
					policy = &RetrySettings{Enabled: &enabled}
				}
				f = mainOpenRecoveryWithRetry(t, store, ref, options, &clock, 0, policy)
				before, exists, err := f.h.Task(bg, task.ID)
				wantRestarts := uint64(1)
				if mode == "clean-close" {
					wantRestarts = 0
				}
				if err != nil || !exists || before.Restarts != wantRestarts {
					t.Fatal("fixture crash/clean-close marker", before, err)
				}
				if err := f.h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := mainInterruptionQuiescent(ctx, f.h); err != nil {
					t.Fatal("fixture did not settle its admitted work", err)
				}
				handle, err := f.h.Submission(bg, sub.ID())
				if err != nil {
					t.Fatal(err)
				}
				status, err := handle.Status(bg)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "backoff":
					record, exists, err := f.h.Task(bg, task.ID)
					if err != nil || !exists {
						t.Fatal(err)
					}
					if record.State.Status != "sleeping" || record.State.Until == nil || *record.State.Until != 1060000 || record.State.Checkpoint["phase"] != "retry" || record.Restarts != 0 {
						t.Errorf("crash retry must sleep until 1060000 and clear restarts: %+v", record)
					}
					state, err := f.h.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					generation := f.live(state)["generation"]
					want := JSON{"attempt": 1, "retry": JSON{"at": 1060000, "error": "The host stopped while the model was answering, on attempt 1"}}
					if !equalJSONValue(generation, want) {
						t.Errorf("crash retry live projection: got %v; want %v", generation, want)
					}
					wake, err := f.h.Wake(bg)
					if err != nil || wake.Active || wake.At == nil || *wake.At != 1060000 {
						t.Errorf("crash retry wake: %+v, %v", wake, err)
					}
					if calls.Load() != 1 {
						t.Errorf("crash retry dispatched before backoff: %d requests", calls.Load())
					}
				case "disabled":
					if status.Status != "unanswered" || status.Reason != "interrupted" {
						t.Errorf("disabled crash retry must end interrupted: %+v", status)
					}
					if calls.Load() != 1 {
						t.Errorf("disabled crash retry dispatched again: %d requests", calls.Load())
					}
				case "clean-close":
					if status.Status != "done" || calls.Load() != 2 {
						t.Errorf("clean close must resend without charging disabled retries: %+v, requests=%d", status, calls.Load())
					}
				}
			})
		})
	}
}
