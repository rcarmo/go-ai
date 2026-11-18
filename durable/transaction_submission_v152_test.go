package durable

import (
	"context"
	"fmt"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// v1.5.2 harness-submit-in-commit.test.ts: a completed input points at its
// transcript entry, and an unknown submission is absent inside a transaction.
func TestSourceV152TxSubmissionReadsPlacedInput(t *testing.T) {
	bg := context.Background()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- terminal("answer")
		close(ch)
		return ch
	})
	h := openHarness(t, store, options)
	root := root(t, h, ref)
	admitted, err := h.Commit(bg, func(tx *Tx) (any, error) {
		return root.SubmitIn(tx, Input{Type: "input", Content: "hi"})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := admitted.(ID)
	handle, err := h.Submission(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if _, err := handle.Wait(wait); err != nil {
		t.Fatal(err)
	}
	type observation struct {
		status       string
		conversation ID
		kind         string
	}
	got, err := h.Commit(bg, func(tx *Tx) (any, error) {
		submission, found, err := tx.Submission(id)
		if err != nil || !found || submission.Entry == 0 {
			return nil, fmt.Errorf("missing placed input: %+v, %v, %v", submission, found, err)
		}
		entry, found, err := tx.Entry(submission.Entry)
		if err != nil || !found {
			return nil, fmt.Errorf("missing transcript entry: %+v, %v, %v", entry, found, err)
		}
		return observation{submission.Status, entry.Conversation, entry.Kind}, nil
	})
	if want := (observation{"done", root.ID(), "pi.user"}); err != nil || got != want {
		t.Fatalf("committed input = %#v, %v; want %#v", got, err, want)
	}
	missing, err := h.Commit(bg, func(tx *Tx) (any, error) {
		_, found, err := tx.Submission(999_999)
		return found, err
	})
	if err != nil || missing != false {
		t.Fatalf("unknown submission = %v, %v; want absent", missing, err)
	}
}
