package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Native control for the pre-charge reopen/Close/reopen prerequisite of
// f9a26365 harness-restart-failure.test.ts:596-619. It does not resume or assert
// charge-once itself; those source outcome/observer assertions remain open.
func TestMainInterruptedMarkerSurvivesCleanClose(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		var calls atomic.Int64
		stream := &mainRecoveryStream{events: make(chan goai.Event, 1)}
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls.Add(1)
			go func() { <-ctx.Done(); stream.close() }()
			return stream.events
		})
		var clock atomic.Int64
		clock.Store(1000)
		enabled, maximum, delay := true, 3, int64(1)
		policy := &RetrySettings{Enabled: &enabled, MaxRetries: &maximum, BaseDelayMs: &delay}
		f := mainOpenRecoveryWithRetry(t, b.store, ref, options, &clock, 0, policy)
		if _, err := f.c.Submit(bg, Input{Content: "hi"}); err != nil {
			t.Fatal(err)
		}
		f.partial(stream, strings.Repeat("z", 64))
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
			t.Fatal(err)
		}
		store := f.crash()
		for reopen := 0; reopen < 2; reopen++ {
			f = mainOpenRecoveryWithRetry(t, store, ref, options, &clock, 0, policy)
			record, ok, err := f.h.Task(bg, task.ID)
			if err != nil || !ok || record.Restarts != 1 || record.State.Outcome != nil || calls.Load() != 1 {
				t.Fatal("pre-charge marker lost, consumed or dispatched", record, calls.Load(), err)
			}
			if text, ok := mainRecoveryText(f.live(f.stored())); !ok || text != strings.Repeat("z", 64) {
				t.Fatal("pre-charge stored partial changed", text)
			}
			if err := f.h.Close(bg); err != nil {
				t.Fatal(err)
			}
			if reopen == 0 {
				store = utf16ReopenStore(t, store)
			}
		}
	})
}
