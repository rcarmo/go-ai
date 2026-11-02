package durable

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func mainRecoveryStepFuture(t *testing.T, f *mainRecoveryHarness) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		wake, err := f.h.Wake(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !wake.Active && wake.At != nil {
			f.clock.Store(*wake.At)
			if err := f.h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			return
		}
		if err := ctx.Err(); err != nil {
			t.Fatal("no future recovery wake", err)
		}
		runtime.Gosched()
	}
}

// Controlled streams advance only actual future wake deadlines. No task or
// retry state is fabricated, and no view callback acknowledgement is inserted.
func mainRecoveryRetryStream(t *testing.T, f *mainRecoveryHarness, streams <-chan *mainRecoveryStream) *mainRecoveryStream {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	for {
		select {
		case stream := <-streams:
			return stream
		default:
		}
		wake, err := f.h.Wake(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !wake.Active && wake.At != nil {
			f.clock.Store(*wake.At)
			if err := f.h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
		}
		if err := ctx.Err(); err != nil {
			t.Fatal("recovery dispatch did not arrive", err)
		}
		runtime.Gosched()
	}
}
