//go:build cgo

package durable

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
)

func TestInternalSQLiteGroupPostCommitReceipts(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	batches, _ := groupEntryBatches(t, s, 3)
	appendSeries := s.appendFrames
	calls, frames := 0, 0
	s.appendFrames = func(group []storageFrame) error {
		calls++
		frames = len(group)
		return appendSeries(group)
	}
	received := 0
	result, err := s.commitBatches(bg, batches, func(index int, seq uint64) error {
		if index != received || seq != uint64(index+1) || s.state.Seq != 3 {
			return errors.New("unordered receipt or incomplete adoption")
		}
		// BEGIN cannot succeed inside an unfinished SQLite transaction. This
		// probes the actual connection after each receipt, not just a hook flag.
		if err := s.exec("BEGIN IMMEDIATE"); err != nil {
			return err
		}
		if err := s.exec("ROLLBACK"); err != nil {
			return err
		}
		received++
		return nil
	}, false)
	if err != nil || result.refused != nil || result.stored != 3 || received != 3 || calls != 1 || frames != 3 {
		t.Fatalf("result=%+v receipts=%d calls=%d frames=%d err=%v", result, received, calls, frames, err)
	}
}

func TestInternalSQLiteGroupFaultSettlement(t *testing.T) {
	for _, prefix := range []string{"", "worker_"} {
		for _, mode := range []string{"statement-rollback", "commit-unknown", "rollback-unconfirmed"} {
			t.Run(prefix+mode, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "state.db")
				s, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = s.Close(bg) }()
				batches, ids := groupEntryBatches(t, s, 3)
				ordinal, high := s.ordinal, s.state.HighWater
				var statements []string
				if mode == "commit-unknown" {
					statements = []string{"PRAGMA foreign_keys=ON", "CREATE TABLE guard_parent(id INTEGER PRIMARY KEY)", "CREATE TABLE guard_child(id INTEGER REFERENCES guard_parent(id) DEFERRABLE INITIALLY DEFERRED)", "CREATE TRIGGER fail_group AFTER INSERT ON frames WHEN NEW.kind=2 BEGIN INSERT INTO guard_child VALUES(999); END"}
				} else {
					action := "ABORT"
					if mode == "rollback-unconfirmed" {
						action = "ROLLBACK"
					}
					// The first batch's frame was inserted before this failure.
					statements = []string{fmt.Sprintf("CREATE TRIGGER fail_group BEFORE INSERT ON frames WHEN NEW.ordinal=%d BEGIN SELECT RAISE(%s,'group failure'); END", ordinal+2, action)}
				}
				for _, sql := range statements {
					if err := s.exec(sql); err != nil {
						t.Fatal(err)
					}
				}
				received := 0
				result, err := s.commitBatches(bg, batches, func(int, uint64) error { received++; return nil }, false)
				if err == nil || received != 0 || result.refused != nil || s.ordinal != ordinal || s.state.Seq != 0 || s.state.HighWater != high {
					t.Fatalf("failed call result=%+v receipts=%d ordinal=%d err=%v", result, received, s.ordinal, err)
				}
				if mode == "statement-rollback" {
					var rejected *StorageRejected
					if !errors.As(err, &rejected) || errors.Is(err, ErrPoisoned) {
						t.Fatalf("confirmed rollback misclassified: %v", err)
					}
					if _, err := s.Snapshot(bg); err != nil {
						t.Fatal("confirmed rollback stopped store", err)
					}
				} else {
					if !errors.Is(err, ErrPoisoned) {
						t.Fatalf("uncertain failure not poisoned: %v", err)
					}
					if _, err := s.Snapshot(bg); !errors.Is(err, ErrPoisoned) {
						t.Fatal("uncertain store still readable", err)
					}
				}
				if err := s.Close(bg); err != nil {
					t.Fatal(err)
				}
				s, err = OpenSQLite(path, SQLiteOptions{Prefix: prefix})
				if err != nil {
					t.Fatal(err)
				}
				state, err := s.Snapshot(bg)
				if err != nil || state.Seq != 0 || len(state.Entries) != 0 || state.HighWater != high {
					t.Fatalf("rollback recovery=%+v err=%v", state, err)
				}
				if err := s.exec("DROP TRIGGER fail_group"); err != nil {
					t.Fatal(err)
				}
				result, err = s.commitBatches(bg, batches, nil, false)
				if err != nil || result.stored != 3 || result.refused != nil || result.lastSeq != 3 {
					t.Fatalf("retry=%+v err=%v", result, err)
				}
				id, err := s.MintID(bg)
				if err != nil || id <= ids[2] {
					t.Fatal("allocator reused a reserved id", id, err)
				}
			})
		}
	}
}
