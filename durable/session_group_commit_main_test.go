package durable

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// Done records that admission reached its blocking select, without sleeps.
type mainCommitAdmissionContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *mainCommitAdmissionContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestMainSessionUnpublishedReadBarriers(t *testing.T) {
	for _, kind := range []string{"entry", "conversation"} {
		t.Run(kind, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			s, err := OpenSession(store)
			if err != nil {
				store.Close(context.Background())
				t.Fatal(err)
			}
			held, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer func() {
				unblock()
				if err := s.Close(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			original := store.appendFrame
			store.appendFrame = func(typ byte, ordinal, high uint64, payload []byte) error {
				if typ == 2 {
					select {
					case <-held:
					default:
						close(held)
						<-release
					}
				}
				if original != nil {
					return original(typ, ordinal, high, payload)
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var leaked ID
			first := make(chan error, 1)
			go func() {
				_, err := s.Commit(ctx, func(tx *Tx) error {
					var err error
					leaked, err = tx.MintID()
					if err != nil {
						return err
					}
					if kind == "entry" {
						return tx.AppendEntry(Entry{ID: leaked, Conversation: 1, Kind: "note", Value: JSON{"body": "first"}})
					}
					return tx.CreateConversation(Conversation{ID: leaked})
				})
				first <- err
			}()
			select {
			case <-held:
			case <-ctx.Done():
				t.Fatal("first write did not reach storage")
			}
			admission := &mainCommitAdmissionContext{Context: ctx, entered: make(chan struct{})}
			second := make(chan error, 1)
			if kind == "entry" {
				go func() {
					state, err := s.Snapshot(admission)
					if err == nil && state.Entries[leaked].ID != leaked {
						t.Error("published entry missing")
					}
					second <- err
				}()
			} else {
				go func() {
					_, err := s.Commit(admission, func(tx *Tx) error {
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						return tx.AppendEntry(Entry{ID: id, Conversation: leaked, Kind: "note", Value: JSON{"body": "second"}})
					})
					second <- err
				}()
			}
			select {
			case <-admission.entered:
			case <-ctx.Done():
				t.Fatal("dependent operation never attempted admission")
			}
			select {
			case err := <-second:
				t.Fatalf("dependent operation settled before publication: %v", err)
			default:
			}
			unblock()
			select {
			case err := <-first:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("first write stuck")
			}
			select {
			case err := <-second:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("dependent operation stuck")
			}
			snapshot, err := s.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "conversation" {
				if snapshot.Conversations[leaked].ID != leaked || len(snapshot.Entries) != 1 {
					t.Fatal("dependent conversation write missing")
				}
			}
		})
	}
}

// Actual f9a26365 session-group-commit.test.ts case 414, in the part native can express. The source
// admits three parts while the first call is in flight and refuses one of them; native admits one
// writer on the line, so the parts run in admission order. What is measured here is the fence native
// does implement: the call that fails at settlement latches the Session, later operations report the
// failure, and the parts stored before it stay durable while the failed part never lands.
//
// The reported cause is native's own classification ("uncertain storage outcome"), not the storage
// error the source expects; that difference is recorded separately, and the pre-admission refusal path
// (which native does not latch at all) stays as a pending oracle.
func TestMainSessionFailureFenceKeepsEarlierPartsAfterSettlementError(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("connection lost")
	original := store.appendFrame
	stored := 0
	store.appendFrame = func(typ byte, ordinal, high uint64, payload []byte) error {
		if strings.Contains(string(payload), "lost") {
			return cause
		}
		stored++
		if original != nil {
			return original(typ, ordinal, high, payload)
		}
		return nil
	}
	defer func() { store.appendFrame = original }()

	s, err := OpenSession(store)
	if err != nil {
		store.Close(context.Background())
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var a, b ID
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		var err error
		a, err = tx.MintID()
		if err != nil {
			return err
		}
		if err := tx.CreateConversation(Conversation{ID: a}); err != nil {
			return err
		}
		b, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.CreateConversation(Conversation{ID: b})
	}); err != nil {
		t.Fatal(err)
	}

	// The earlier part that must be kept.
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.AppendEntry(Entry{ID: id, Conversation: a, Kind: "note", Value: JSON{"body": "kept"}})
	}); err != nil {
		t.Fatal(err)
	}

	// The part that fails at settlement.
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.AppendEntry(Entry{ID: id, Conversation: b, Kind: "note", Value: JSON{"body": "lost"}})
	}); err == nil {
		t.Fatal("settlement error settled without an error")
	}

	// The Session is latched: a later read reports SessionFailed.
	if _, err := s.Snapshot(ctx); err != nil {
		var failed *SessionFailed
		if !errors.As(err, &failed) {
			t.Fatalf("read after settlement error is not SessionFailed: %v", err)
		}
	} else {
		t.Fatal("read after settlement error returned no error")
	}

	end, err := s.Closed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if end.Reason != "failed" {
		t.Fatalf("session end must report failed: %+v", end)
	}

	if err := s.Close(ctx); err != nil {
		var failed *SessionFailed
		if !errors.As(err, &failed) {
			t.Fatalf("close after failure: %v", err)
		}
	}

	// Earlier parts stayed durable; the failed part never landed.
	reopened := reopenStoreAfterHarnessClose(t, store)
	defer reopened.Close(ctx)
	s2, err := OpenSession(reopened)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close(ctx)
	snap, err := s2.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries := 0
	for _, e := range snap.Entries {
		entries++
		if e.Kind != "note" || e.Value["body"] != "kept" {
			t.Fatalf("the failed part landed: %v", e)
		}
	}
	if entries != 1 {
		t.Fatalf("only the earlier part should be stored, got %d (frames written: %d)", entries, stored)
	}
}
