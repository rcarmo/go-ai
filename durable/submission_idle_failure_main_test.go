package durable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source harness-submit-in-commit.test.ts:120: a failed run may retain queued
// input, but that queue does not make the conversation live for whenIdle.
func TestMainSubmissionSourceWhenIdleAfterFailedRun(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		close(entered)
		go func() {
			defer close(ch)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			m := terminal("").Message
			m.Content = nil
			m.StopReason, m.ErrorMessage = goai.StopReasonError, "Invalid request"
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: m}
		}()
		return ch
	})
	options.Settings = &HarnessSettings{AfterFailedRun: "keep"}
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	c := root(t, h, ref)
	first, err := c.Submit(bg, Input{Type: "input", Content: "first"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("provider did not start")
	}
	queued, err := c.Submit(bg, Input{Type: "input", Content: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	unblock()
	settled := waitSubmission(t, first)
	if settled.Record.Status != "unanswered" || settled.Record.Reason != "model_error" {
		t.Fatalf("first failure %+v", settled.Record)
	}
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if err := h.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if !failedQueueHeld(before, c.ID()) {
		t.Fatal("fixture did not retain failed queue")
	}
	if _, err = c.Submit(bg, Input{Type: "input", Content: "steer", WhenBusy: "steer", WhenIdle: "reject"}); !errors.Is(err, ErrConversationIdle) {
		t.Fatalf("idle after failure: %v", err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatalf("rejection wrote: %d %d %v", before.Seq, after.Seq, err)
	}
	status, err := queued.Status(bg)
	if err != nil || status.Status != "queued" {
		t.Fatalf("queued input: %+v %v", status, err)
	}
	live := mainContinueBoundaryDocument(t, after, c.ID(), "pi.live")
	if len(generationObject(live["run"])) != 0 {
		t.Fatalf("run restarted: %v", live)
	}
}
