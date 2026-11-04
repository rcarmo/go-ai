package durable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source harness-submit-in-commit.test.ts:108,137,159. The public external
// submit path is exercised; caller-owned transaction and tool APIs are separate.
func TestMainSubmissionSourceWhenIdleAdmission(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		select {
		case <-entered:
		default:
			close(entered)
		}
		go func() {
			select {
			case <-release:
				ch <- terminal("done")
			case <-ctx.Done():
			}
			close(ch)
		}()
		return ch
	})
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	c := root(t, h, ref)
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := c.Submit(bg, Input{Type: "input", Content: "hi", WhenBusy: "steer", WhenIdle: "reject"})
	if rejected != nil || !errors.Is(err, ErrConversationIdle) {
		t.Fatalf("idle result: %v %v", rejected, err)
	}
	var idle *ConversationIdle
	if !errors.As(err, &idle) || idle.ConversationID != c.ID() {
		t.Fatalf("idle conversation: %v", err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq || len(after.Submissions) != len(before.Submissions) || len(after.Entries) != len(before.Entries) {
		t.Fatalf("idle wrote: before=%d after=%d err=%v", before.Seq, after.Seq, err)
	}
	if live := mainContinueBoundaryDocument(t, after, c.ID(), "pi.live"); len(generationObject(live["run"])) != 0 {
		t.Fatalf("idle run started: %v", live)
	}
	first, err := c.Submit(bg, Input{Type: "input", Content: "work", RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("model not started")
	}
	steer, err := c.Submit(bg, Input{Type: "input", Content: "steer", WhenBusy: "steer", WhenIdle: "reject"})
	if err != nil {
		t.Fatalf("running steer: %v", err)
	}
	status, err := steer.Status(bg)
	if err != nil || status.Status != "queued" || status.Type != "input" {
		t.Fatalf("steer status %+v %v", status, err)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	inbox := mainContinueBoundaryDocument(t, state, c.ID(), "pi.inbox")
	items := inbox["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["mode"] != "steer" {
		t.Fatalf("steer inbox %v", items)
	}
	if result, err := steer.Abort(bg); err != nil || result != "aborted" {
		t.Fatalf("steer abort %s %v", result, err)
	}
	unblock()
	if got := waitSubmission(t, first); got.Submission.Status != "done" {
		t.Fatal(got)
	}
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if err := c.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	before, err = h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := c.Submit(bg, Input{Type: "input", Content: "work", RequestID: "r1", WhenBusy: "steer", WhenIdle: "reject"})
	if err != nil || duplicate.ID() != first.ID() {
		t.Fatalf("idle dedup %v %v", duplicate, err)
	}
	after, err = h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatalf("dedup wrote: %d %d %v", before.Seq, after.Seq, err)
	}
}
