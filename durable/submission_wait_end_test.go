package durable

import (
	"errors"
	"testing"
	"time"
)

func TestSubmissionPendingPublicationWaitCloseAndPoisonRemoveRegistrations(t *testing.T) {
	for _, ending := range []string{"close", "poison"} {
		t.Run(ending, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				h := taskTestHarness(t, b.store)
				var id ID
				_, err := h.Commit(bg, func(tx *Tx) error {
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
				sub, err := h.Submission(bg, id)
				if err != nil {
					t.Fatal(err)
				}
				result := make(chan error, 1)
				go func() { _, err := sub.Wait(bg); result <- err }()
				deadline := time.After(3 * time.Second)
				for {
					registered := false
					h.session.taskBookkeeping(func() { registered = len(h.submissionWaiters[id]) == 1 })
					if registered {
						break
					}
					select {
					case <-deadline:
						t.Fatal("wait not registered")
					default:
					}
				}
				expected := ErrClosed
				if ending == "close" {
					if err := h.Close(bg); err != nil {
						t.Fatal(err)
					}
				} else {
					expected = ErrPoisoned
					// A real uncertain append poisons storage; the Session rollback path must
					// wake already-admitted waiters even when no publication can be adopted.
					prior := h.session.core.appendFrame
					h.session.core.appendFrame = func(byte, uint64, uint64, []byte) error { return errors.New("uncertain append") }
					_, err := h.Commit(bg, func(tx *Tx) error {
						current := tx.state.Submissions[id]
						current.Status = "done"
						return tx.PutSubmission(current)
					})
					h.session.core.appendFrame = prior
					if !errors.Is(err, ErrPoisoned) {
						t.Fatal(err)
					}
				}
				select {
				case err := <-result:
					if !errors.Is(err, expected) {
						t.Fatal(ending, err)
					}
				case <-time.After(3 * time.Second):
					t.Fatal("pending wait retained", ending)
				}
				h.session.taskBookkeeping(func() {
					if len(h.submissionWaiters) != 0 {
						t.Error("wait registration leaked", ending)
					}
				})
			})
		})
	}
}
