package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSubmissionWaitRegistersAndSettlesFromArbitraryCommitWithoutPolling(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		var id ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			id, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.PutSubmission(Submission{ID: id, Conversation: 1, Type: "write", Status: "pending", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		handle, err := h.Submission(bg, id)
		if err != nil {
			t.Fatal(err)
		}
		caller, cancel := context.WithCancel(bg)
		defer cancel()
		first := make(chan error, 1)
		second := make(chan Settlement, 1)
		failures := make(chan error, 1)
		go func() { _, err := handle.Wait(caller); first <- err }()
		go func() {
			result, err := handle.Wait(bg)
			if err != nil {
				failures <- err
				return
			}
			second <- result
		}()
		deadline := time.After(3 * time.Second)
		for {
			registered := false
			h.session.taskBookkeeping(func() { registered = len(h.submissionWaiters[id]) == 2 })
			if registered {
				break
			}
			select {
			case <-deadline:
				t.Fatal("submission registration absent")
			default:
			}
		}
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			sub := tx.state.Submissions[id]
			sub.Status = "done"
			sub.Value = JSON{"application": "settled"}
			return tx.PutSubmission(sub)
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case final := <-second:
			if final.Submission.Status != "done" || final.Submission.Value["application"] != "settled" {
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
