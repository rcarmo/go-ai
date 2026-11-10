package durable

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// session.ts fail/#poisoned/#enqueue: a storage job keeps its own failure;
// later undelivered jobs inherit SessionFailed even if their callback already
// succeeded or produced a different preparation error before the failure arrived.
func TestSessionV150FailureBoundaryPoisonsLaterUndeliveredOutcomes(t *testing.T) {
	s := gatedSession(t)
	options := DefinitionOptions{Kind: "boundary.read", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }}
	base, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(base, 0, nil, nil); return err }); err != nil {
		t.Fatal(err)
	}
	migrated := make(chan struct{})
	options.Version = 2
	options.Migrate = func(value JSON, _ uint64) (JSON, error) { close(migrated); return value, nil }
	cold, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	cause := reject("source storage refusal")
	s.writer.host.commit = func(context.Context, []Batch, func(commitReceipt)) (commitGroupResult, error) {
		close(entered)
		<-release
		return commitGroupResult{lastSeq: s.readImage.Seq, refused: &commitRefusal{cause}}, nil
	}
	first := make(chan error, 1)
	go func() {
		_, err := s.Commit(bg, func(tx *Tx) error {
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "refused", Value: JSON{}})
		})
		first <- err
	}()
	<-entered
	ownError := errors.New("later callback error")
	later := []chan error{make(chan error, 1), make(chan error, 1)}
	for i := range later {
		ran := make(chan struct{})
		i := i
		go func() {
			_, err := s.Commit(bg, func(*Tx) error {
				close(ran)
				if i == 0 {
					return ownError
				}
				return nil
			})
			later[i] <- err
		}()
		awaitTaskSignal(t, ran)
	}
	readResult := make(chan error, 1)
	go func() { _, _, err := s.SnapshotDefinition(bg, cold, 0, nil); readResult <- err }()
	awaitTaskSignal(t, migrated)
	unblock()
	if err := <-first; err != cause {
		t.Errorf("storage job lost own error identity: %v", err)
	}
	later = append(later, readResult)
	for i, result := range later {
		err := <-result
		var failed *SessionFailed
		if !errors.As(err, &failed) || failed.Cause != cause {
			t.Errorf("later job %d not poisoned with first cause: %v", i, err)
		}
	}
}

// A call that rejects after delivering receipts owns the errors of every one
// of its parts. Those parts are durable; later callback outcomes are poisoned.
func TestSessionV150RejectedCallKeepsEveryOwnPartOutcome(t *testing.T) {
	for _, value := range []any{commitUndefinedValue{}, nil, false, 0, ""} {
		t.Run(fmt.Sprintf("%T", value), func(t *testing.T) {
			s := gatedSession(t)
			id, err := s.MintID(bg)
			if err != nil {
				t.Fatal(err)
			}
			def, err := DefineDocument(DefinitionOptions{Kind: "boundary.parts", Scope: "session", Family: true, Version: 1, Initial: func(JSON) (JSON, error) { return JSON{}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err != nil {
				t.Fatal(err)
			}
			cause := newFailure(value)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			calls := 0
			commit := s.writer.host.commit
			s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
				calls++
				if calls == 1 {
					close(entered)
					<-release
					return commit(ctx, batches, receive)
				}
				result, err := commit(ctx, batches, receive)
				if err != nil {
					return result, err
				}
				return result, cause
			}
			first := make(chan error, 1)
			go func() {
				_, err := s.Commit(bg, func(tx *Tx) error {
					return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "prefix", Value: JSON{}})
				})
				first <- err
			}()
			<-entered
			parts := []chan error{make(chan error, 1), make(chan error, 1)}
			for i := range parts {
				ran := make(chan struct{})
				i := i
				go func() {
					_, err := s.Commit(bg, func(tx *Tx) error {
						key := fmt.Sprint(i)
						_, err := tx.AcquireDocument(def, 0, &key, nil)
						close(ran)
						return err
					})
					parts[i] <- err
				}()
				awaitTaskSignal(t, ran)
			}
			later := make(chan error, 1)
			ran := make(chan struct{})
			go func() {
				_, err := s.Commit(bg, func(*Tx) error { close(ran); return errors.New("later preparation") })
				later <- err
			}()
			awaitTaskSignal(t, ran)
			queued := make(chan struct{})
			read := make(chan error, 1)
			go func() { _, err := s.Snapshot(&documentGateContext{Context: bg, queued: queued}); read <- err }()
			awaitTaskSignal(t, queued)
			unblock()
			if err := <-first; err != nil {
				t.Fatal("earlier durable prefix was poisoned", err)
			}
			for i, part := range parts {
				if err := <-part; err != cause {
					t.Errorf("own part %d lost rejection identity: %v", i, err)
				}
			}
			for _, result := range []chan error{later, read} {
				var failed *SessionFailed
				if err := <-result; !errors.As(err, &failed) || failed.Cause != cause {
					t.Errorf("later outcome lost first cause: %v", err)
				}
			}
			if calls != 2 {
				t.Errorf("storage calls=%d, want 2", calls)
			}
			if _, err := s.Closed(bg); err != nil {
				t.Fatal(err)
			}
			reopened := reopenStoreAfterHarnessClose(t, s.store)
			defer reopened.Close(bg)
			state, err := reopened.Snapshot(bg)
			if err != nil || len(state.Entries) != 1 || len(state.Documents) != 2 {
				t.Fatal("receipted parts did not survive reopen", state, err)
			}
		})
	}
}
