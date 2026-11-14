package durable

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func mainSQLiteSnapshotSession(tb testing.TB) *Session {
	tb.Helper()
	store, err := OpenSQLite(filepath.Join(tb.TempDir(), "state.db"), SQLiteOptions{})
	if err != nil {
		tb.Fatal(err)
	}
	seedRootConversation(tb, store)
	seedRootConversation(tb, store)
	s, err := OpenSession(store)
	if err != nil {
		store.Close(context.Background())
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := s.Close(context.Background()); err != nil {
			tb.Error(err)
		}
	})
	_, err = s.Commit(context.Background(), func(tx *Tx) (any, error) {
		for i := 0; i < 64; i++ {
			id, err := tx.MintID()
			if err != nil {
				return nil, err
			}
			if err = tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "fixture", Value: JSON{"body": strings.Repeat("x", 4096)}}); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

func TestMainSQLiteAdoptedTaskSnapshot(t *testing.T) {
	s := mainSQLiteSnapshotSession(t)
	err := s.readTasks(context.Background(), func(state Snapshot) error {
		if reflect.ValueOf(state.Entries).Pointer() != reflect.ValueOf(s.core.state.Entries).Pointer() {
			t.Error("internal task read copied the adopted SQLite image")
		}
		if len(state.Entries) != 64 {
			t.Errorf("entries=%d", len(state.Entries))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	public, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for id, entry := range public.Entries {
		legacyEntryValue(entry)["body"] = "changed"
		public.Entries[id] = entry
		break
	}
	if err = s.readTasks(context.Background(), func(state Snapshot) error {
		for _, entry := range state.Entries {
			if legacyEntryValue(entry)["body"] != strings.Repeat("x", 4096) {
				t.Error("public snapshot aliases adopted image")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var published ID
	_, err = s.Commit(context.Background(), func(tx *Tx) (any, error) {
		var err error
		published, err = tx.MintID()
		if err != nil {
			return nil, err
		}
		return nil, tx.AppendEntry(Entry{ID: published, Conversation: 1, Kind: "fixture", Value: JSON{"body": "published"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.readTasks(context.Background(), func(state Snapshot) error {
		if state.Entries[published].Value["body"] != "published" {
			t.Error("read missed adopted commit")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type mainSQLiteSnapshotAdapter struct {
	*SQLiteStorage
	reads   atomic.Int64
	failure error
}

func (s *mainSQLiteSnapshotAdapter) Snapshot(ctx context.Context) (Snapshot, error) {
	s.reads.Add(1)
	if s.failure != nil {
		return Snapshot{}, s.failure
	}
	return s.SQLiteStorage.Snapshot(ctx)
}

func TestMainSQLiteTaskSnapshotPreservesAdapter(t *testing.T) {
	base, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, base)
	adapter := &mainSQLiteSnapshotAdapter{SQLiteStorage: base}
	s, err := OpenSession(adapter)
	if err != nil {
		base.Close(context.Background())
		t.Fatal(err)
	}
	defer s.Close(context.Background())
	for i := 0; i < 2; i++ {
		if err := s.readTasks(context.Background(), func(Snapshot) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if got := adapter.reads.Load(); got != 2 {
		t.Errorf("Snapshot adapter reads=%d want2", got)
	}
	adapter.failure = errors.New("snapshot gate")
	if err := s.readTasks(context.Background(), func(Snapshot) error { return nil }); !errors.Is(err, adapter.failure) {
		t.Fatalf("adapter error lost: %v", err)
	}
}

func TestMainSQLiteTaskSnapshotAdmission(t *testing.T) {
	s := mainSQLiteSnapshotSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.readTasks(ctx, func(Snapshot) error { t.Error("canceled callback ran"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.readTasks(context.Background(), func(Snapshot) error { t.Error("closed callback ran"); return nil }); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func BenchmarkMainSQLiteTaskSnapshot(b *testing.B) {
	s := mainSQLiteSnapshotSession(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := s.readTasks(context.Background(), func(state Snapshot) error {
			if len(state.Entries) != 64 {
				b.Fatal("lost entries")
			}
			return nil
		}); err != nil {
			b.Fatal(err)
		}
	}
}
