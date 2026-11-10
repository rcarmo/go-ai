package durable

import (
	"context"
	"sync"
	"testing"
	"time"
)

func holdSessionOutcome(t *testing.T, s *Session) (func(), <-chan error) {
	t.Helper()
	id, err := s.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	commit := s.writer.host.commit
	var held sync.Once
	s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		held.Do(func() { close(entered); <-release })
		return commit(ctx, batches, receive)
	}
	result := make(chan error, 1)
	go func() {
		_, err := s.Commit(bg, func(tx *Tx) error {
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "bound", Value: JSON{}})
		})
		result <- err
	}()
	<-entered
	return unblock, result
}

// session.ts commitInternal/#enqueue: a no-write/no-hit internal job is exempt
// from outcome order, but ordinary jobs still follow the earlier writer.
func TestSessionV150InternalNoHitOutcomeIsExempt(t *testing.T) {
	s := gatedSession(t)
	unblock, first := holdSessionOutcome(t, s)
	defer unblock()
	result := make(chan error, 1)
	go func() { _, err := s.commitInternal(bg, func(*Tx) error { return nil }); result <- err }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		unblock()
		<-first
		<-result
		t.Fatal("no-hit internal outcome waited for the writer")
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
}

// Pinned session-group-commit.test.ts:920. The first held writer and 255
// completed line parts consume the bound; job 257 cannot start until delivery.
func TestSessionV150OutstandingJobsBound(t *testing.T) {
	s := gatedSession(t)
	unblock, first := holdSessionOutcome(t, s)
	defer unblock()
	ran := make(chan struct{}, 255)
	results := make(chan error, 256)
	for i := 0; i < 255; i++ {
		go func() { _, err := s.Commit(bg, func(*Tx) error { ran <- struct{}{}; return nil }); results <- err }()
	}
	for i := 0; i < 255; i++ {
		select {
		case <-ran:
		case <-time.After(3 * time.Second):
			t.Fatal("job before bound did not start")
		}
	}
	extra := make(chan struct{})
	go func() { _, err := s.Commit(bg, func(*Tx) error { close(extra); return nil }); results <- err }()
	select {
	case <-extra:
		unblock()
		t.Fatal("job 257 started before an outcome was delivered")
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-extra:
	default:
		t.Fatal("backpressure did not reopen")
	}
	if len(s.outcomeSlots) != 0 || s.writer.unpublishedCount() != 0 {
		t.Fatal("line retained idle outcomes or unpublished keys")
	}
}

// A gate hit removes the internal exemption even when the callback stages no
// writes; a disjoint write also removes it despite touching no pending key.
func TestSessionV150InternalHitOrWritesAreFenced(t *testing.T) {
	for _, writes := range []bool{false, true} {
		name := "hit"
		if writes {
			name = "writes"
		}
		t.Run(name, func(t *testing.T) {
			s := gatedSession(t)
			unblock, first := holdSessionOutcome(t, s)
			defer unblock()
			result := make(chan error, 1)
			queued := make(chan struct{})
			ctx := context.Context(bg)
			var def *DocumentDefinition
			if writes {
				var err error
				def, err = DefineDocument(DefinitionOptions{Kind: "internal.disjoint", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				ctx = &documentGateContext{Context: bg, queued: queued}
			}
			go func() {
				_, err := s.commitInternal(ctx, func(tx *Tx) error {
					if !writes {
						_, err := tx.ScanConversations(ConversationQuery{}, 1, nil)
						return err
					}
					_, err := tx.AcquireDocument(def, 0, nil, nil)
					close(queued)
					return err
				})
				result <- err
			}()
			awaitTaskSignal(t, queued)
			select {
			case err := <-result:
				t.Fatalf("internal %s outcome escaped: %v", name, err)
			case <-time.After(30 * time.Millisecond):
			}
			unblock()
			if err := <-first; err != nil {
				t.Fatal(err)
			}
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		})
	}
}
