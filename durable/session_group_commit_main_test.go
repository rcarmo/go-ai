package durable

import (
	"context"
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
			store, err := NewMemory()
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
