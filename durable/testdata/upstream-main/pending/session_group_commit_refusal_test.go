package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Known-failing oracle for f9a26365 session-group-commit.test.ts case 414, "makes every outcome after
// a fatal failure SessionFailed, and keeps the earlier ones", for the refusal half of that case.
//
// Source assertion: a batch the storage refuses fails the Session, so every outcome after it is
// SessionFailed and `session.closed` is `{ kind: "failed", error: <refusal> }`.
//
// Measured native baseline (revision cf49780, count=1, 1.10s): the refusal is reported as
// `durable: rejected: invalid write count` (a StorageRejected: no write was admitted), the Session is
// not latched, and the operation admitted after it succeeds. Native latches only on an error during
// settlement, which passes as `TestMainSessionFailureFenceKeepsEarlierPartsAfterSettlementError` in
// `durable/session_group_commit_main_test.go`.
//
// Two faithfulness limits recorded, not resolved: native has no predicate-based backend refusal inside
// a call, so the nearest expressible refusal is the storage's own admission refusal; and the source's
// three parts admitted while the first call is in flight cannot be reproduced because native admits one
// writer on the line.
//
// Recipe:
//
//	make test PACKAGES=./durable TEST_FLAGS="-overlay <run>/overlay.json -v -run '^TestPendingGroupCommitRefusalFailsTheSession' -count=1 -timeout=45s"
func TestPendingGroupCommitRefusalFailsTheSession(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(store)
	if err != nil {
		store.Close(context.Background())
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var a, b, c, d ID
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		for _, target := range []*ID{&a, &b, &c, &d} {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			*target = id
			if err := tx.CreateConversation(Conversation{ID: id}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The earlier part that must be kept.
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.appendEntryRecord(Entry{ID: id, Conversation: a, Kind: "note", Value: JSON{"body": "kept"}})
	}); err != nil {
		t.Fatal(err)
	}

	// The batch the storage refuses.
	refused, err := s.Commit(ctx, func(tx *Tx) error {
		for n := 0; n <= DefaultLimits().MaxWrites; n++ {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.appendEntryRecord(Entry{ID: id, Conversation: b, Kind: "refuse", Value: JSON{"body": "x"}}); err != nil {
				return err
			}
		}
		return nil
	})
	_ = refused
	if err == nil {
		t.Fatal("refused part settled without a refusal")
	}
	var rejected *StorageRejected
	if !errors.As(err, &rejected) {
		t.Fatalf("refused part is not a storage refusal: %v", err)
	}
	cause := err

	// Source assertion, unmet natively: every outcome after the failure is SessionFailed.
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.appendEntryRecord(Entry{ID: id, Conversation: c, Kind: "note", Value: JSON{"body": "after"}})
	}); err != nil {
		var failed *SessionFailed
		if !errors.As(err, &failed) {
			t.Fatalf("operation after the failure is not SessionFailed: %v", err)
		}
	} else {
		t.Fatal("operation after a refused batch succeeded: the Session was not latched")
	}

	// Retained assertion: the session end reports failed with the first error.
	end, err := s.Closed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if end.Reason != "failed" || !errors.Is(end.Error, cause) {
		t.Fatalf("session end must report failed with the refusal: %+v", end)
	}

	// Retained assertion: the earlier part stayed durable, the refused part and the part after it did not.
	if err := s.Close(ctx); err != nil {
		var failed *SessionFailed
		if !errors.As(err, &failed) && !errors.Is(err, cause) {
			t.Fatalf("close after failure: %v", err)
		}
	}
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
	kept := 0
	for _, e := range snap.Entries {
		if e.Kind == "note" && e.Value["body"] == "kept" {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("the earlier part must be kept and the refused/later parts absent, got %d kept", kept)
	}
}
