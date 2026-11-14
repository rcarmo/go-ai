package durable

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// Unlike a hold inside appendFrame, this leaves the storage line available.
// The raw image demonstrably lacks the pending conversation, so a Session scan
// must not substitute a raw backend read for admission behind its creation.
func TestMainSessionUnpublishedScanBarriers(t *testing.T) {
	for _, kind := range []string{"global-conversations", "new-conversation-entries"} {
		t.Run(kind, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			seedRootConversation(t, store)
			s, err := OpenSession(store)
			if err != nil {
				store.Close(context.Background())
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			held, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var workers sync.WaitGroup
			defer func() {
				unblock()
				cancel()
				if err := s.Close(context.Background()); err != nil {
					t.Error(err)
				}
				workers.Wait()
			}()
			var leaked ID
			first := make(chan error, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				_, err := s.Commit(ctx, func(tx *Tx) (any, error) {
					var err error
					leaked, err = tx.MintID()
					if err != nil {
						return nil, err
					}
					if err := tx.createConversationRecord(Conversation{ID: leaked}); err != nil {
						return nil, err
					}
					close(held)
					select {
					case <-release:
						return nil, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				})
				first <- err
			}()
			select {
			case <-held:
			case <-ctx.Done():
				t.Fatal("creation did not reach its hold")
			}
			raw, err := store.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, exists := raw.Conversations[leaked]; exists {
				t.Fatal("pending conversation already stored")
			}
			admission := &mainCommitAdmissionContext{Context: ctx, entered: make(chan struct{})}
			second := make(chan error, 1)
			workers.Add(1)
			go func() {
				defer workers.Done()
				if kind == "global-conversations" {
					page, err := s.ScanConversations(admission, ConversationQuery{}, 10, nil)
					if err == nil {
						found := false
						for _, item := range page.Items {
							found = found || item.ID == leaked
						}
						if !found {
							err = fmt.Errorf("global scan omitted pending creation")
						}
					}
					second <- err
					return
				}
				page, err := s.ScanEntries(admission, EntryQuery{Conversation: leaked}, 10, nil)
				if err == nil && (len(page.Items) != 0 || page.Next != nil) {
					err = fmt.Errorf("new conversation scan returned unexpected entries or continuation")
				}
				second <- err
			}()
			select {
			case <-admission.entered:
			case <-ctx.Done():
				t.Fatal("scan never attempted admission")
			}
			select {
			case err := <-second:
				t.Fatalf("scan settled before creation: %v", err)
			default:
			}
			unblock()
			for _, result := range []<-chan error{first, second} {
				select {
				case err := <-result:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("creation or scan did not settle")
				}
			}
		})
	}
}
