package durable

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type coldConversationStorage struct {
	Storage
	reads   atomic.Int64
	witness Conversation
	missing bool
	failure error
}

func (a *coldConversationStorage) Conversation(ctx context.Context, id ID) (Conversation, bool, error) {
	if id != a.witness.ID {
		return a.Storage.Conversation(ctx, id)
	}
	a.reads.Add(1)
	if a.failure != nil {
		return Conversation{}, false, a.failure
	}
	if a.missing {
		return Conversation{}, false, nil
	}
	return a.witness, true, nil
}

// Public Storage witness of the pinned conversation table-read path at
// transaction.ts:303 and harness.ts conversation(). The adapter's scoped
// answer is authoritative even when the loaded image lacks the lookup key.
func TestSourceV160ColdConversationReads(t *testing.T) {
	for _, surface := range []string{"session", "transaction", "line", "harness"} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failure=%t", surface, fail), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(bg, 5*time.Second)
				defer cancel()
				store, err := newTestSQLite(t)
				if err != nil {
					t.Fatal(err)
				}
				seedRootConversation(t, store)
				a := &coldConversationStorage{Storage: store, witness: Conversation{ID: 999, Parent: 1, ParentAt: 17}}
				if fail {
					a.failure = errors.New("cold conversation backend failure")
				}
				var result Conversation
				var found bool
				var closed func(context.Context) (SessionEnd, error)
				repeated := func(read func(ID) (Conversation, bool, error)) error {
					for range 2 {
						var err error
						result, found, err = read(a.witness.ID)
						if err != nil {
							return err
						}
						if !found || result != a.witness {
							return fmt.Errorf("cold read %+v found %t", result, found)
						}
					}
					return nil
				}
				if surface == "harness" {
					h := openHarness(t, a, Options{})
					closed = h.Closed
					var handle *ConversationHandle
					handle, err = h.Conversation(ctx, a.witness.ID)
					if handle != nil {
						found = true
						if handle.ID() != a.witness.ID {
							t.Fatalf("conversation handle ID %d; want %d", handle.ID(), a.witness.ID)
						}
					}
				} else {
					s, openErr := OpenSession(a)
					if openErr != nil {
						t.Fatal(openErr)
					}
					defer s.Close(bg)
					closed = s.Closed
					switch surface {
					case "session":
						result, found, err = s.Conversation(ctx, a.witness.ID)
					case "transaction":
						_, err = s.Commit(ctx, func(tx *Tx) (any, error) { return nil, repeated(tx.Conversation) })
					case "line":
						err = s.ReadOnLine(ctx, func(read *ReadLine) error { return repeated(read.Conversation) })
					}
				}
				if fail {
					if !errors.Is(err, a.failure) || found {
						t.Fatalf("cold conversation failure: found %t, err %v", found, err)
					}
					end, err := closed(ctx)
					if err != nil || end.Reason != "failed" || end.Failure.AsError() != a.failure {
						t.Fatalf("closed: %+v, err %v", end, err)
					}
				} else if err != nil || !found || surface != "harness" && result != a.witness {
					t.Fatalf("cold conversation: got %+v, found %t, err %v", result, found, err)
				}
				if got := a.reads.Load(); got != 1 {
					t.Fatalf("scoped Storage conversation reads = %d; want one", got)
				}
			})
		}
	}
}

func TestSourceV160ColdConversationMissIsScoped(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	id := mint(t, store)
	a := &coldConversationStorage{Storage: store, witness: Conversation{ID: id}, missing: true}
	s, err := OpenSession(a)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
		for range 2 {
			if record, found, err := tx.Conversation(id); record != (Conversation{}) || found || err != nil {
				return nil, fmt.Errorf("cold miss: %+v, found=%t, err=%v", record, found, err)
			}
		}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if a.reads.Load() != 1 {
		t.Fatalf("cold negative reads = %d; want one", a.reads.Load())
	}
	if err := s.ReadOnLine(bg, func(read *ReadLine) error {
		if record, found, err := read.Conversation(id); record != (Conversation{}) || found || err != nil {
			return fmt.Errorf("next scope: %+v, found=%t, err=%v", record, found, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if a.reads.Load() != 2 {
		t.Fatalf("cold negative reads = %d; want two", a.reads.Load())
	}
}
