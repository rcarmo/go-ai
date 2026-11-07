package durable

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func mainSQLiteCommitBatch(t *testing.T, s *SQLiteStorage, body string) (ID, Batch) {
	t.Helper()
	id, err := s.MintID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return id, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "fixture", Value: JSON{"body": body}}}}}
}

// Source: 81f3cd41 store-storage-commit.test.ts, rollback versus failed COMMIT.
// Native stores use framed writes, not Chord's row/outbox schema.
func TestMainSQLiteStatementRollbackContinues(t *testing.T) {
	for _, prefix := range []string{"", "worker_"} {
		t.Run(prefix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.db")
			s, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
			if err != nil {
				t.Fatal(err)
			}
			seedRootConversation(t, s)
			defer s.Close(context.Background())
			first, seed := mainSQLiteCommitBatch(t, s, "seed")
			last, err := s.Apply(context.Background(), seed)
			if err != nil {
				t.Fatal(err)
			}
			second, batch := mainSQLiteCommitBatch(t, s, "retry")
			if err = s.exec("CREATE TRIGGER fail_append BEFORE INSERT ON frames WHEN NEW.kind=2 BEGIN SELECT RAISE(ABORT,'statement failed'); END;"); err != nil {
				t.Fatal(err)
			}
			if seq, err := s.Apply(context.Background(), batch); err == nil || seq != 0 || errors.Is(err, ErrPoisoned) {
				t.Fatalf("confirmed rollback seq=%d error=%v", seq, err)
			}
			snap, err := s.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if snap.Seq != last || len(snap.Entries) != 1 || snap.Entries[first].Value["body"] != "seed" {
				t.Fatalf("rollback altered adopted state: seq=%d entries=%v", snap.Seq, snap.Entries)
			}
			if _, ok := snap.Entries[second]; ok {
				t.Fatal("failed append became visible")
			}
			if err = s.exec("DROP TRIGGER fail_append"); err != nil {
				t.Fatal(err)
			}
			if seq, err := s.Apply(context.Background(), batch); err != nil || seq != last+1 {
				t.Fatalf("retry seq=%d err=%v", seq, err)
			}
			if err = s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
			if err != nil {
				t.Fatal(err)
			}
			seedRootConversation(t, reopened)
			defer reopened.Close(context.Background())
			snap, err = reopened.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if snap.Seq != last+1 || len(snap.Entries) != 2 || snap.Entries[second].Value["body"] != "retry" {
				t.Fatal("reopen lost prefix/retry", snap.Seq, snap.Entries)
			}
			if next, err := reopened.MintID(context.Background()); err != nil || next <= second {
				t.Fatalf("reservation reused: id=%d error=%v", next, err)
			}
		})
	}
}

func TestMainSQLiteFailedCommitStillPoisons(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, s)
	defer s.Close(context.Background())
	id, batch := mainSQLiteCommitBatch(t, s, "uncertain")
	// The INSERT succeeds. A deferred FK check rejects only COMMIT.
	for _, sql := range []string{"PRAGMA foreign_keys=ON", "CREATE TABLE guard_parent(id INTEGER PRIMARY KEY)", "CREATE TABLE guard_child(id INTEGER REFERENCES guard_parent(id) DEFERRABLE INITIALLY DEFERRED)", "CREATE TRIGGER fail_commit AFTER INSERT ON frames WHEN NEW.kind=2 BEGIN INSERT INTO guard_child VALUES(999); END"} {
		if err = s.exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	if seq, err := s.Apply(context.Background(), batch); seq != 0 || !errors.Is(err, ErrPoisoned) {
		t.Fatalf("COMMIT error seq=%d err=%v", seq, err)
	}
	if _, err = s.Snapshot(context.Background()); !errors.Is(err, ErrPoisoned) {
		t.Fatal("poisoned read", err)
	}
	if _, err = s.MintID(context.Background()); !errors.Is(err, ErrPoisoned) {
		t.Fatal("poisoned mint", err)
	}
	if err = s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, reopened)
	defer reopened.Close(context.Background())
	snap, err := reopened.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Seq != 1 || len(snap.Entries) != 0 {
		t.Fatal("failed COMMIT persisted", snap.Seq)
	}
	if err = reopened.exec("DROP TRIGGER fail_commit"); err != nil {
		t.Fatal(err)
	}
	if seq, err := reopened.Apply(context.Background(), batch); err != nil || seq != 2 {
		t.Fatalf("reopened retry seq=%d err=%v id=%d", seq, err, id)
	}
}

func TestMainSQLiteUnconfirmedRollbackStillPoisons(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, s)
	defer s.Close(context.Background())
	_, batch := mainSQLiteCommitBatch(t, s, "rollback")
	// SQLite already ends the transaction; explicit rollback confirmation fails.
	if err = s.exec("CREATE TRIGGER end_transaction BEFORE INSERT ON frames WHEN NEW.kind=2 BEGIN SELECT RAISE(ROLLBACK,'ended transaction'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Apply(context.Background(), batch); !errors.Is(err, ErrPoisoned) {
		t.Fatal("unconfirmed rollback admitted", err)
	}
	if _, err = s.Snapshot(context.Background()); !errors.Is(err, ErrPoisoned) {
		t.Fatal("poisoned read", err)
	}
}
