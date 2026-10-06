package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestHostSettingsAtomicRefreshCurrentRetryDecisionAndDetachedInputs(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls.Add(1)
			ch := make(chan goai.Event, 1)
			close(entered)
			go func() {
				<-release
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
				close(ch)
			}()
			return ch
		})
		h := openHarness(t, b.store, options)
		sub, err := root(t, h, ref).Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		disabled := false
		temperature := 0.5
		if err := h.SetSettings(bg, HarnessSettings{Retry: &RetrySettings{Enabled: &disabled}, Stream: RequestSettings{Temperature: &temperature}}); err != nil {
			t.Fatal(err)
		}
		disabled = true
		temperature = 0.9
		current := h.resolvedSettings(RequestSettings{})
		if current.Retry.Enabled || current.Temperature == nil || *current.Temperature != 0.5 {
			t.Fatal("settings not detached", current)
		}
		releaseTaskGate(release)
		if result := waitSubmission(t, sub); result.Submission.Status != "failed" || calls.Load() != 1 {
			t.Fatal(result, calls.Load())
		}
		before, _ := h.Snapshot(bg)
		negative := -1
		if err := h.SetSettings(bg, HarnessSettings{Stream: RequestSettings{MaxRetries: &negative}}); err == nil {
			t.Fatal("invalid update accepted")
		}
		after, _ := h.Snapshot(bg)
		if before.Seq != after.Seq || h.resolvedSettings(RequestSettings{}).Retry.Enabled {
			t.Fatal("host settings mutated persistent state")
		}
	})
}
