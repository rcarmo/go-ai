package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubmissionWaitRegistersAndSettlesFromArbitraryCommitWithoutPolling(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		// The pinned transaction cannot mark a queued write done without
		// placement. A placed input and its answer exercise the same waiter
		// notification through a canonical settlement.
		var submission SubmissionRecord
		_, err := h.Commit(bg, func(tx *Tx) (any, error) {
			entry, err := tx.AppendEntry(1, EntryDraft{"kind": "app.note"})
			if err != nil {
				return nil, err
			}
			submission, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "placed", Entry: entry.ID})
			return nil, err
		})
		if err != nil {
			t.Fatal(err)
		}
		id := submission.ID
		handle, err := h.Submission(bg, id)
		if err != nil || handle == nil {
			t.Fatal(err)
		}
		caller, cancel := context.WithCancel(bg)
		defer cancel()
		first := make(chan error, 1)
		second := make(chan Settlement, 1)
		failures := make(chan error, 1)
		progress := make(chan struct{}, 16)
		go func() { _, err := handle.Wait(submissionWaitContext{caller, progress}); first <- err }()
		go func() {
			result, err := handle.Wait(submissionWaitContext{bg, progress})
			if err != nil {
				failures <- err
				return
			}
			second <- result
		}()
		awaitSubmissionWaiters(t, h, id, 2, progress)
		cancel()
		if err := watchReceive(t, first); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		answer, err := h.Commit(bg, func(tx *Tx) (any, error) {
			entry, err := tx.AppendEntry(1, EntryDraft{"kind": "app.note"})
			if err != nil {
				return nil, err
			}
			return entry.ID, tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: entry.ID})
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case final := <-second:
			if final.Record.ID != id || final.Record.Status != "done" || final.Record.Entry != submission.Entry || final.Record.Answer != answer.(ID) {
				t.Fatal(final)
			}
		case err := <-failures:
			t.Fatal(err)
		case <-time.After(3 * time.Second):
			t.Fatal("adoption did not wake wait")
		}
		h.session.taskBookkeeping(func() {
			if len(h.submissionWaiters) != 0 {
				t.Error("settled/cancelled waiter retained", len(h.submissionWaiters))
			}
		})
	})
}
