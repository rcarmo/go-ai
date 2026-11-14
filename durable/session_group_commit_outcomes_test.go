package durable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// Consolidated from session_outcome_fence_v150_test.go.

// session.ts #enqueue: only the line part ends at admission. A commit with no
// writes still delivers behind the outcomes admitted before it.
func TestSessionV150ReadOnlyCommitDeliversBehindEarlierWriter(t *testing.T) {
	s := gatedSession(t)
	id, err := s.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	commit := s.writer.host.commit
	s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		close(entered)
		<-release
		return commit(ctx, batches, receive)
	}
	first := make(chan error, 1)
	go func() {
		_, err := s.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "fence", Value: JSON{}})
		})
		first <- err
	}()
	<-entered
	ran := make(chan struct{})
	second := make(chan error, 1)
	go func() { _, err := s.Commit(bg, func(*Tx) (any, error) { close(ran); return nil, nil }); second <- err }()
	awaitTaskSignal(t, ran)
	select {
	case err := <-second:
		t.Fatalf("read-only outcome overtook held writer: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}

// Consolidated from session_backpressure_v150_test.go.

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
		_, err := s.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "bound", Value: JSON{}})
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
		go func() {
			_, err := s.Commit(bg, func(*Tx) (any, error) { ran <- struct{}{}; return nil, nil })
			results <- err
		}()
	}
	for i := 0; i < 255; i++ {
		select {
		case <-ran:
		case <-time.After(3 * time.Second):
			t.Fatal("job before bound did not start")
		}
	}
	extra := make(chan struct{})
	go func() {
		_, err := s.Commit(bg, func(*Tx) (any, error) { close(extra); return nil, nil })
		results <- err
	}()
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

// Consolidated from session_failure_boundary_v150_test.go.

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
	if _, err = s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(base, 0, nil, nil); return nil, err }); err != nil {
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
		_, err := s.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "refused", Value: JSON{}})
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
			_, err := s.Commit(bg, func(*Tx) (any, error) {
				close(ran)
				if i == 0 {
					return nil, ownError
				}
				return nil, nil
			})
			later[i] <- err
		}()
		awaitTaskSignal(t, ran)
	}
	readResult := make(chan error, 1)
	go func() { _, _, err := detachedDefinitionSnapshot(s, bg, cold, 0, nil); readResult <- err }()
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
				_, err := s.Commit(bg, func(tx *Tx) (any, error) {
					return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "prefix", Value: JSON{}})
				})
				first <- err
			}()
			<-entered
			parts := []chan error{make(chan error, 1), make(chan error, 1)}
			for i := range parts {
				ran := make(chan struct{})
				i := i
				go func() {
					_, err := s.Commit(bg, func(tx *Tx) (any, error) {
						key := fmt.Sprint(i)
						_, err := tx.AcquireDocument(def, 0, &key, nil)
						close(ran)
						return nil, err
					})
					parts[i] <- err
				}()
				awaitTaskSignal(t, ran)
			}
			later := make(chan error, 1)
			ran := make(chan struct{})
			go func() {
				_, err := s.Commit(bg, func(*Tx) (any, error) { close(ran); return nil, errors.New("later preparation") })
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

// Consolidated from session_failure_value_v150_test.go.

// TestV150SessionFailureCarriesOriginalRejectionValue ports the original failure
// semantics: a storage rejection carries its value verbatim. The rejecting call is
// rejected with that value, later calls get a SessionFailed whose cause is the
// same value, and the Session end reports it as the failure error. The original
// failure slot is `{ error: unknown }`, so present null/undefined/falsy values are
// failures, not absences.
// Source: session/session.ts:274 (#failure: { readonly error: unknown }), 1642, 1740, 1839-1847.
func TestV150SessionFailureCarriesOriginalRejectionValue(t *testing.T) {
	for _, value := range []any{commitUndefinedValue{}, nil, false, 0, ""} {
		t.Run(reflect.ValueOf(value).Kind().String(), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				s, err := OpenSession(b.store)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close(bg)
				core := b.store.(nativeStorage).nativeCore()
				if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
					id, e := tx.mintID()
					if e != nil {
						return nil, e
					}
					return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "setup", Value: JSON{}})
				}); err != nil {
					t.Fatal(err)
				}
				core.appendFrame = func(byte, uint64, uint64, []byte) error {
					return newFailure(value)
				}

				// The rejecting call keeps the original value.
				_, err = s.Commit(bg, func(tx *Tx) (any, error) {
					id, e := tx.mintID()
					if e != nil {
						return nil, e
					}
					return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "reject", Value: JSON{}})
				})
				var failure *Failure
				if !errors.As(err, &failure) || !reflect.DeepEqual(failure.Value(), value) {
					t.Fatalf("rejecting call lost the value: %v (%#v)", err, err)
				}
				if err == nil {
					t.Fatal("rejection accepted")
				}

				// A later call reports the first cause, value intact.
				var failed *SessionFailed
				if _, err := s.Commit(bg, func(tx *Tx) (any, error) { return nil, nil }); !errors.As(err, &failed) {
					t.Fatalf("later call not fenced with SessionFailed: %v", err)
				} else if !reflect.DeepEqual(failed.Failure().Value(), value) {
					t.Fatalf("SessionFailed lost the cause value: %#v", failed.Failure().Value())
				}

				// The Session end carries the same value.
				end, err := s.Closed(bg)
				if err != nil {
					t.Fatal(err)
				}
				if end.Reason != "failed" || end.Failure == nil || !reflect.DeepEqual(end.Failure.Value(), value) {
					t.Fatalf("session end lost the value: %+v", end)
				}
			})
		})
	}
}

// TestV150SessionFailureKeepsErrorIdentity pins the error-valued case: when the
// rejection is a Go error its identity survives unchanged, so existing
// errors.Is/errors.As classification keeps working.
func TestV150SessionFailureKeepsErrorIdentity(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		core := b.store.(nativeStorage).nativeCore()
		cause := errors.New("lost acknowledgement")
		core.appendFrame = func(byte, uint64, uint64, []byte) error {
			return cause
		}
		_, err = s.Commit(bg, func(tx *Tx) (any, error) {
			id, e := tx.mintID()
			if e != nil {
				return nil, e
			}
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "reject", Value: JSON{}})
		})
		if !errors.Is(err, cause) {
			t.Fatalf("error cause identity lost: %v", err)
		}
		end, err := s.Closed(bg)
		if err != nil {
			t.Fatal(err)
		}
		if end.Reason != "failed" || !errors.Is(end.Error, cause) {
			t.Fatalf("session end lost the cause: %+v", end)
		}
	})
}

// Consolidated from session_group_commit_refusal_boundary_main_test.go.

// Source-derived P04 control for f9a26365 session-group-commit.test.ts:414.
// This injects a confirmed StorageRejected at the native appendFrame boundary,
// before the frame reaches SQLite. It is not the source's in-call predicate
// backend or its multiple concurrently admitted parts. In particular it does not
// establish SQLite rollback semantics. It distinguishes this boundary from the
// pre-admission MaxWrites rejection in the pending source414 approximation.
func TestMainSessionConfirmedAppendRefusalLatches(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	original := store.appendFrame
	refusal := &StorageRejected{Reason: "simulated confirmed append refusal"}
	hits := 0
	store.appendFrame = func(typ byte, ordinal, high uint64, payload []byte) error {
		if typ == 2 && strings.Contains(string(payload), "p04-refused-marker") {
			hits++
			return refusal
		}
		return original(typ, ordinal, high, payload)
	}
	seedRootConversation(t, store)
	s, err := OpenSession(store)
	if err != nil {
		store.appendFrame = original
		_ = store.Close(context.Background())
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer func() {
		_ = s.Close(context.Background())
		store.appendFrame = original
	}()

	appendNote := func(body string) error {
		_, err := s.Commit(ctx, func(tx *Tx) (any, error) {
			id, err := tx.mintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"body": body}})
		})
		return err
	}
	if err := appendNote("p04-kept-marker"); err != nil {
		t.Fatalf("earlier commit: %v", err)
	}
	err = appendNote("p04-refused-marker")
	var failed *SessionFailed
	if err != refusal || !errors.As(s.failure(), &failed) || failed.Cause != refusal {
		t.Fatalf("append refusal must latch with the injected cause: %T %v", err, err)
	}
	if hits != 1 {
		t.Fatalf("refusal hook calls: got %d, want 1", hits)
	}
	if _, err := s.Snapshot(ctx); !errors.Is(err, refusal) || !errors.As(err, &failed) {
		t.Fatalf("later read must report SessionFailed with the original cause: %v", err)
	}
	if err := appendNote("p04-later-marker"); !errors.Is(err, refusal) || !errors.As(err, &failed) {
		t.Fatalf("later write must report SessionFailed with the original cause: %v", err)
	}
	end, err := s.Closed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if end.Reason != "failed" || !errors.Is(end.Error, refusal) {
		t.Fatalf("closed must retain the failure cause: %+v", end)
	}
	_ = s.Close(ctx)
	store.appendFrame = original
	reopened := reopenStoreAfterHarnessClose(t, store)
	defer reopened.Close(context.Background())
	snapshot, err := reopened.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var kept, refused, later int
	for _, entry := range snapshot.Entries {
		switch legacyEntryValue(entry)["body"] {
		case "p04-kept-marker":
			kept++
		case "p04-refused-marker":
			refused++
		case "p04-later-marker":
			later++
		}
	}
	if kept != 1 || refused != 0 || later != 0 {
		t.Fatalf("reopened entries: kept=%d refused=%d later=%d", kept, refused, later)
	}
}

// A real SQLite statement failure with confirmed rollback, exercised through
// Session settlement. The trigger applies only to the marked commit frame;
// appendSeries rolls its transaction back and returns StorageRejected. This is
// still one Session commit per call, not the source414 multi-part predicate.
func TestMainSessionSQLiteConfirmedRollbackLatches(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	s, err := OpenSession(store)
	if err != nil {
		_ = store.Close(context.Background())
		t.Fatal(err)
	}
	defer func() { _ = s.Close(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	appendNote := func(body string) error {
		_, err := s.Commit(ctx, func(tx *Tx) (any, error) {
			id, err := tx.mintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"body": body}})
		})
		return err
	}
	if err := appendNote("p04-sqlite-kept"); err != nil {
		t.Fatalf("earlier commit: %v", err)
	}
	advances := make(chan sessionPublication, 1)
	endings := make(chan WatchEnd, 1)
	if _, err := s.observeStoredCommits(sessionCommitObserver{
		advance: func(_ context.Context, value sessionPublication) error {
			advances <- value
			return nil
		},
		end: func(value WatchEnd) error {
			endings <- value
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Use SQLite's actual frame table and statement-abort semantics; the
	// transaction rollback is performed by appendSeries, not by this test.
	if err := store.exec(`CREATE TRIGGER p04_refuse BEFORE INSERT ON frames
		WHEN NEW.kind=2 AND instr(CAST(NEW.payload AS TEXT), 'p04-sqlite-refused') > 0
		BEGIN SELECT RAISE(ABORT, 'p04 injected SQLite failure'); END`); err != nil {
		t.Fatal(err)
	}
	err = appendNote("p04-sqlite-refused")
	var failed *SessionFailed
	var rejected *StorageRejected
	if !errors.As(err, &rejected) || err != rejected || !errors.As(s.failure(), &failed) || failed.Cause != rejected || errors.Is(err, ErrPoisoned) {
		t.Fatalf("confirmed rollback classification: %T %v", err, err)
	}
	if rejected.Reason != "SQLite frame rejected" {
		t.Fatalf("confirmed rollback reason: %q", rejected.Reason)
	}
	if _, err := s.Snapshot(ctx); !errors.Is(err, rejected) || !errors.As(err, &failed) {
		t.Fatalf("later read must retain the first rejection: %v", err)
	}
	if err := appendNote("p04-sqlite-later"); !errors.Is(err, rejected) || !errors.As(err, &failed) {
		t.Fatalf("later write must retain the first rejection: %v", err)
	}
	end, err := s.Closed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if end.Reason != "failed" || !errors.Is(end.Error, rejected) {
		t.Fatalf("closed must retain the rejection: %+v", end)
	}
	select {
	case observerEnd := <-endings:
		if observerEnd.Reason != "failed" || !errors.Is(observerEnd.Error, rejected) {
			t.Fatalf("observer must end with the rejection: %+v", observerEnd)
		}
	case <-ctx.Done():
		t.Fatal("stored observer did not end")
	}
	if len(advances) != 0 || len(endings) != 0 {
		t.Fatalf("refusal caused unexpected observer delivery: advances=%d extra endings=%d", len(advances), len(endings))
	}
	_ = s.Close(ctx)
	reopened := reopenStoreAfterHarnessClose(t, store)
	defer reopened.Close(context.Background())
	snapshot, err := reopened.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var kept, refused, later int
	for _, entry := range snapshot.Entries {
		switch legacyEntryValue(entry)["body"] {
		case "p04-sqlite-kept":
			kept++
		case "p04-sqlite-refused":
			refused++
		case "p04-sqlite-later":
			later++
		}
	}
	if kept != 1 || refused != 0 || later != 0 {
		t.Fatalf("reopened entries: kept=%d refused=%d later=%d", kept, refused, later)
	}
}
