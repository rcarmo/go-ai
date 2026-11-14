package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Source Session failure is final: failure-driven shutdown must not turn its
// first cause into an ordinary closed error at later Harness entry points.
func TestV150FailurePrecedesHarnessClosedErrors(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	harness := openHarness(t, store, Options{})
	conversation := root(t, harness, ModelRef{})
	id, err := harness.session.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	store.appendFrame = func(byte, uint64, uint64, []byte) error { return errors.New("injected uncertain storage failure") }
	_, err = harness.session.Commit(bg, func(tx *Tx) (any, error) {
		return nil, tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "failure", Value: JSON{}})
	})
	if !errors.Is(err, ErrPoisoned) {
		t.Fatalf("initial own storage failure=%v", err)
	}
	var first *SessionFailed
	if !errors.As(harness.session.failure(), &first) || first.Cause != err {
		t.Fatal("first storage error was not latched exactly", harness.session.failure())
	}
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if _, err := harness.session.Closed(ctx); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		call func() error
	}{
		{"resume", func() error { return harness.Resume(bg) }},
		{"wait-task", func() error { _, err := harness.WaitForTask(bg, id); return err }},
		{"wait-idle", func() error { return harness.WaitForIdle(bg) }},
		{"root", func() error { _, err := harness.Root(bg, AgentChange{}); return err }},
		{"commit", func() error {
			_, err := harness.commitTasks(bg, conversation.ID(), func(*Tx) error { t.Error("failed commit callback ran"); return nil })
			return err
		}},
		{"context", func() error { _, err := conversation.ContextView(bg, 0); return err }},
		{"scan", func() error { _, err := conversation.Entries(bg, EntryQuery{}, 1, nil); return err }},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			var failed *SessionFailed
			if !errors.As(err, &failed) || failed.Cause != first.Cause {
				t.Fatalf("failure identity replaced: %v", err)
			}
		})
	}
}
