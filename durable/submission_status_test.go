package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestSubmissionReferenceStatusAndAbortDispositionScopeQueuedWrites(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				close(entered)
				go func() { <-release; ch <- terminal("answer"); close(ch) }()
			} else {
				ch <- terminal("extra")
				close(ch)
			}
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		root := root(t, h, ref)
		first, err := root.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		queued, err := root.Submit(bg, Input{Content: "queued"})
		if err != nil {
			t.Fatal(err)
		}
		write, err := root.Submit(bg, Input{Type: "write", Content: "never placed"})
		if err != nil {
			t.Fatal(err)
		}
		status, err := queued.Status(bg)
		if err != nil || status.Status != "queued" {
			t.Fatal(status, err)
		}
		status.Value["content"] = "mutated"
		// Session applications may create a queued submission without
		// Harness admission/inbox placement; abort must still settle it.
		var raw ID
		_, err = h.CommitTasks(bg, root.ID(), func(tx *Tx) error {
			var err error
			raw, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.PutSubmission(Submission{ID: raw, Conversation: root.ID(), Type: "write", Status: "pending", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := h.AbortSubmission(bg, raw); err != nil || result != "aborted" {
			t.Fatal("raw queued abort", result, err)
		}
		if absent, err := h.Submission(bg, ID(MaxID)); err != nil || absent != nil {
			t.Fatal("unknown submission lookup", absent, err)
		}
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := h.AbortSubmission(bg, queued.ID(), ID(MaxID)); err != nil || result != "not_found" {
			t.Fatal(result, err)
		}
		if result, err := h.AbortSubmission(bg, ID(MaxID)); err != nil || result != "not_found" {
			t.Fatal(result, err)
		}
		if result, err := first.Abort(bg); err != nil || result != "already_placed" {
			t.Fatal(result, err)
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq {
			t.Fatal("lookup disposition wrote", err)
		}
		for _, sub := range []*SubmissionHandle{queued, write} {
			if result, err := sub.Abort(bg); err != nil || result != "aborted" {
				t.Fatal(result, err)
			}
			if result, err := sub.Abort(bg); err != nil || result != "settled" {
				t.Fatal(result, err)
			}
			status, err := sub.Status(bg)
			if err != nil || status.Status != "unanswered" || status.Reason != "aborted" || status.Value["errorCode"] != "aborted" {
				t.Fatal(status, err)
			}
		}
		releaseTaskGate(release)
		if final := waitSubmission(t, first); final.Submission.Status != "done" {
			t.Fatal(final)
		}
		if result, err := first.Abort(bg); err != nil || result != "settled" {
			t.Fatal(result, err)
		}
		if calls != 1 {
			t.Fatal("aborted queued input dispatched", calls)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range state.Entries {
			if entry.Value["role"] == string(goai.RoleUser) {
				var message MessageReceipt
				if err := fromObject(entry.Value, &message, h.session.limits); err != nil {
					t.Fatal(err)
				}
				for _, block := range message.Content {
					if block.Text == "never placed" || block.Text == "queued" {
						t.Fatal("withdrawn submission placed", entry)
					}
				}
			}
		}
	})
}
