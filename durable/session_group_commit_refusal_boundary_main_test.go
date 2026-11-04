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
