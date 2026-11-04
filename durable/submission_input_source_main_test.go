package durable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source harness-submissions.test.ts:57,82 and harness/types.ts:68–96.
// This also checks the accepted legacy Go spelling as a compatibility control.
func TestMainSubmissionSourceInputAdmissionAndBusyModes(t *testing.T) {
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
	first, err := c.Submit(bg, Input{Type: "input", Content: "one", RequestID: "one"})
	if err != nil {
		t.Fatalf("source input admission: %v", err)
	}
	status, err := first.Status(bg)
	if err != nil || status.Type != "input" || status.Status != "placed" || status.Entry == 0 {
		t.Fatalf("initial: %+v %v", status, err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		unblock()
		t.Fatal("provider did not start")
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Submit(bg, Input{Type: "input", Content: "refuse", WhenBusy: "reject"}); !errors.Is(err, ErrConversationBusy) {
		t.Fatalf("busy reject: %v", err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatalf("rejected input wrote: %d %d %v", before.Seq, after.Seq, err)
	}
	dup, err := c.Submit(bg, Input{Type: "input", Content: "ignored", RequestID: "one", WhenBusy: "reject"})
	if err != nil || dup.ID() != first.ID() {
		t.Fatalf("dedup: %v %v", dup, err)
	}
	for _, mode := range []string{"followUp", "follow-up", "steer"} {
		sub, err := c.Submit(bg, Input{Type: "input", Content: mode, WhenBusy: mode})
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		status, err := sub.Status(bg)
		if err != nil || status.Status != "queued" || status.Type != "input" {
			t.Fatalf("%s status %+v %v", mode, status, err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		value := mainContinueBoundaryDocument(t, state, c.ID(), "pi.inbox")
		items, ok := value["items"].([]any)
		if !ok || len(items) == 0 {
			t.Fatalf("%s inbox %v", mode, value)
		}
		item, ok := items[len(items)-1].(map[string]any)
		if !ok {
			t.Fatalf("%s item %T", mode, items[len(items)-1])
		}
		want := "followUp"
		if mode == "steer" {
			want = "steer"
		}
		if item["mode"] != want {
			t.Fatalf("%s queued mode %v want %s", mode, item["mode"], want)
		}
		if result, err := sub.Abort(bg); err != nil || result != "aborted" {
			t.Fatalf("%s abort %v %v", mode, result, err)
		}
	}
	unblock()
	if got := waitSubmission(t, first); got.Submission.Status != "done" {
		t.Fatalf("settlement %+v", got)
	}
}
