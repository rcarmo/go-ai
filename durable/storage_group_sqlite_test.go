package durable

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
		if index != received || seq != uint64(index+2) || s.state.Seq != 4 {
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
				s, err := openFrameSQLiteFixture(t, path, SQLiteOptions{Prefix: prefix})
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
				if err == nil || received != 0 || result.refused != nil || s.ordinal != ordinal || s.state.Seq != 1 || s.state.HighWater != high {
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
				s, err = openFrameSQLiteFixture(t, path, SQLiteOptions{Prefix: prefix})
				if err != nil {
					t.Fatal(err)
				}
				state, err := s.Snapshot(bg)
				if err != nil || state.Seq != 1 || len(state.Entries) != 0 || state.HighWater != high {
					t.Fatalf("rollback recovery=%+v err=%v", state, err)
				}
				if err := s.exec("DROP TRIGGER fail_group"); err != nil {
					t.Fatal(err)
				}
				result, err = s.commitBatches(bg, batches, nil, false)
				if err != nil || result.stored != 3 || result.refused != nil || result.lastSeq != 4 {
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

// A real SQL insert is held after it succeeds but before the surrounding
// group transaction commits. The gate does not change the writes or receipts.
type sqliteInsertGate struct {
	sqliteConnection
	entered, release chan struct{}
	begin, commit    int
	once             sync.Once
}

func (g *sqliteInsertGate) ExecContext(ctx context.Context, statement string, args ...any) (sql.Result, error) {
	result, err := g.sqliteConnection.ExecContext(ctx, statement, args...)
	if err != nil {
		return result, err
	}
	switch statement {
	case "BEGIN IMMEDIATE":
		g.begin++
	case "COMMIT":
		g.commit++
	}
	if strings.HasPrefix(statement, `INSERT INTO "entries"`) {
		g.once.Do(func() {
			close(g.entered)
			<-g.release
		})
	}
	return result, nil
}

// pi v1.7.0 store-storage-commit.test.ts:129–156,718–739 and
// store-storage.test.ts:332–372: one Durable commit group, ordered receipts,
// no partial read, and the same latest/as-of head marker after file reopen.
// This compares the native SQLite equivalent, not Chord's outbox or archive.
func TestSQLiteGroupedVisibilityAndReopenedHead(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "head.sqlite")
	writer, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	var baseline uint64
	seed, err := writer.Commit(ctx, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}}, func(_ int, seq uint64) { baseline = seq })
	if err != nil || seed.Kind() != "stored" || seed.Stored != 1 || baseline == 0 {
		t.Fatal("root seed", seed, baseline, err)
	}
	first, marker, last := mint(t, writer), mint(t, writer), mint(t, writer)
	batches := [][]StorageWrite{
		{{Type: "entry", Value: Entry{ID: first, Conversation: 1, Kind: "k"}}},
		{{Type: "entry", Value: Entry{ID: marker, Conversation: 1, Kind: "k", Head: first}}},
		{{Type: "entry", Value: Entry{ID: last, Conversation: 1, Kind: "k"}}},
	}
	viewer, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close(bg)

	gate := &sqliteInsertGate{sqliteConnection: writer.db, entered: make(chan struct{}), release: make(chan struct{})}
	writer.db = gate
	var workers sync.WaitGroup
	defer func() {
		releaseTaskGate(gate.release)
		cancel()
		workers.Wait()
	}()
	type outcome struct {
		result CommitResult
		err    error
	}
	done := make(chan outcome, 1)
	var received atomic.Int64
	var receipts []publicSQLiteReceipt // read only after the commit worker reports completion
	workers.Add(1)
	go func() {
		defer workers.Done()
		result, err := writer.Commit(ctx, batches, func(i int, seq uint64) {
			receipts = append(receipts, publicSQLiteReceipt{i, seq})
			received.Add(1)
		})
		done <- outcome{result, err}
	}()
	awaitTaskSignal(t, gate.entered)
	query := EntryQuery{Conversation: 1, Order: Ascending}
	if page, err := viewer.ScanEntries(ctx, query, 10, nil); err != nil || len(page.Items) != 0 || received.Load() != 0 {
		t.Fatal("pre-commit viewer did not see a readable empty prefix with no new receipts", page, received.Load(), err)
	}
	if entry, found, err := viewer.FindLatestHeadMarker(ctx, 1, 0); err != nil || found {
		t.Fatal("uncommitted group leaked its head marker", entry, found, err)
	}

	type reading struct {
		page Page[Entry]
		err  error
	}
	reader := &mainCommitAdmissionContext{Context: ctx, entered: make(chan struct{})}
	read := make(chan reading, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		page, err := writer.ScanEntries(reader, query, 10, nil)
		read <- reading{page, err}
	}()
	awaitTaskSignal(t, reader.entered)
	select {
	case partial := <-read:
		t.Fatal("same-store read bypassed the in-flight group", partial.page, partial.err)
	default:
	}
	releaseTaskGate(gate.release)
	if got := watchReceive(t, done); got.err != nil || got.result.Kind() != "stored" || got.result.Stored != 3 || got.result.Refused != nil {
		t.Fatal("group did not store all batches", got.result, got.err)
	}
	if want := []publicSQLiteReceipt{{0, baseline + 1}, {1, baseline + 2}, {2, baseline + 3}}; !slices.Equal(receipts, want) || gate.begin != 1 || gate.commit != 1 {
		t.Fatal("wrong grouped transaction or ordered receipts", receipts, want, gate.begin, gate.commit)
	}
	checkRows := func(page Page[Entry], err error) {
		t.Helper()
		if err != nil || page.Next != nil || len(page.Items) != 3 || page.Items[0].ID != first || page.Items[1].ID != marker || page.Items[2].ID != last {
			t.Fatal("reader did not see the entire committed group in order", page, err)
		}
	}
	got := watchReceive(t, read)
	checkRows(got.page, got.err)
	checkRows(viewer.ScanEntries(ctx, query, 10, nil))
	checkHead := func(store Storage) {
		t.Helper()
		if head, found, err := store.FindLatestHeadMarker(ctx, 1, 0); err != nil || !found || head.ID != marker || head.Head != first {
			t.Fatal("latest head marker was not the committed marker", head, found, err)
		}
		if head, found, err := store.FindLatestHeadMarker(ctx, 1, first); err != nil || found {
			t.Fatal("head lookup crossed its as-of entry", head, found, err)
		}
	}
	checkHead(writer)
	checkHead(viewer)
	workers.Wait()
	if err := viewer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	checkHead(reopened)
	for i, id := range []ID{first, marker, last} {
		entry, found, err := reopened.Entry(ctx, id)
		if err != nil || !found || entry.Entry.ID != id || entry.CommitSeq != baseline+1+uint64(i) {
			t.Fatal("wrong persisted entry or sequence after file reopen", id, entry, found, err)
		}
	}
}
