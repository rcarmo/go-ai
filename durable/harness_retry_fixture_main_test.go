package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// mainSlowRetry matches official f9a26365 harness-deferred-progress.test.ts:29-35.
// Only fixtures using source slowSetup opt in; ordinary fixtures keep defaults.
func mainSlowRetry() *RetrySettings {
	enabled, retries, delay := true, 5, int64(1)
	return &RetrySettings{Enabled: &enabled, MaxRetries: &retries, BaseDelayMs: &delay}
}

func mainAssertSlowRetry(t *testing.T, h *Harness) {
	t.Helper()
	got := h.resolvedSettings(RequestSettings{}).Retry
	if !got.Enabled || got.MaxRetries != 5 || got.BaseDelayMs != 1 {
		t.Fatalf("source slowSetup retry: got %+v; want enabled/max5/base1ms", got)
	}
}

func mainOpenSlowRecovery(t *testing.T, store Storage, ref ModelRef, options Options, clock *atomic.Int64, window float64) *mainRecoveryHarness {
	t.Helper()
	f := mainOpenRecoveryWithRetry(t, store, ref, options, clock, window, mainSlowRetry())
	mainAssertSlowRetry(t, f.h)
	return f
}

func TestMainHarnessRetryFixtureDefaultsUnchanged(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			t.Error("unexpected provider call")
			return nil
		})
		var clock atomic.Int64
		clock.Store(1000)
		f := mainOpenRecovery(t, b.store, ref, options, &clock, 400)
		got := f.h.resolvedSettings(RequestSettings{}).Retry
		want := RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 2000, MaxDelayMs: 60000}
		if got != want || defaultHarnessSettings().Retry != want {
			t.Fatalf("ordinary fixture or production defaults changed: got %+v, default %+v; want %+v", got, defaultHarnessSettings().Retry, want)
		}
	})
}
