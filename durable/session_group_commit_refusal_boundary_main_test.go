package durable

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

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
		_, err := s.Commit(ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"body": body}})
		})
		return err
	}
	if err := appendNote("p04-kept-marker"); err != nil {
		t.Fatalf("earlier commit: %v", err)
	}
	err = appendNote("p04-refused-marker")
	var failed *SessionFailed
	if !errors.As(err, &failed) || !errors.Is(err, refusal) {
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
		switch entry.Value["body"] {
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
		_, err := s.Commit(ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"body": body}})
		})
		return err
	}
	if err := appendNote("p04-sqlite-kept"); err != nil {
		t.Fatalf("earlier commit: %v", err)
	}
	advances := make(chan CommitPublication, 1)
	endings := make(chan WatchEnd, 1)
	if _, err := s.observeStoredCommits(sessionCommitObserver{
		advance: func(_ context.Context, value CommitPublication) error {
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
	if !errors.As(err, &failed) || !errors.As(err, &rejected) || errors.Is(err, ErrPoisoned) {
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
		switch entry.Value["body"] {
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
