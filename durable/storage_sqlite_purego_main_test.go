package durable

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Deliberately untagged: OpenSQLite and its types must exist with CGO_ENABLED=0.
func mainPureGoSQLiteOpen(t *testing.T, path string) *SQLiteStorage {
	t.Helper()
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error("SQLite cleanup:", err)
		}
	})
	return store
}

func mainPureGoSQLiteExternal(t *testing.T, path string) *sql.DB {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: path}
	connector, err := newSQLiteConnector(uri.String())
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error("external SQLite cleanup:", err)
		}
	})
	return db
}

func TestMainSQLitePureGoLiteralPathsOwnExpectedFile(t *testing.T) {
	for _, tc := range []struct{ name, filename string }{
		{"question", "question?.sqlite"},
		{"fragment", "fragment#.sqlite"},
		{"percent", "percent%23%3F%25%.sqlite"},
		{"spaces-unicode", "space 雪 λ.sqlite"},
		{"uri-memory", "state.sqlite?mode=memory&cache=shared#literal"},
		{"uri-pragmas", "state.sqlite?_pragma=journal_mode%28MEMORY%29&_pragma=synchronous%28OFF%29&_pragma=busy_timeout%280%29#literal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			path := filepath.Join(dir, tc.filename)
			store := mainPureGoSQLiteOpen(t, path)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
				t.Fatalf("expected private regular database, got %v", info.Mode())
			}
			// A second pathname for the expected inode must hit the lifetime lock,
			// not merely the in-process pathname registry or a URI-selected file.
			alias := filepath.Join(dir, "same-inode.sqlite")
			if err := os.Link(path, alias); err != nil {
				t.Fatal(err)
			}
			duplicate, err := OpenSQLite(alias, SQLiteOptions{})
			if duplicate != nil {
				_ = duplicate.Close(ctx)
			}
			if !errors.Is(err, ErrOwned) || duplicate != nil {
				t.Fatalf("expected-file inode lock bypassed: %v", err)
			}
			if err := os.Remove(alias); err != nil {
				t.Fatal(err)
			}
			id, err := store.MintID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			seq, err := store.Apply(ctx, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "fixture", Value: JSON{"body": tc.filename}}}}})
			if err != nil || seq != 2 {
				t.Fatalf("commit seq=%d: %v", seq, err)
			}
			if err := store.Close(ctx); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			var header [16]byte
			_, readErr := io.ReadFull(file, header[:])
			closeErr := file.Close()
			if readErr != nil || closeErr != nil || string(header[:]) != "SQLite format 3\x00" {
				t.Fatalf("expected filename is not a SQLite database: header=%q read=%v close=%v", header, readErr, closeErr)
			}
			reopened := mainPureGoSQLiteOpen(t, path)
			state, err := reopened.Snapshot(ctx)
			if err != nil || state.Seq != seq || state.HighWater != uint64(id) || len(state.Entries) != 1 || state.Entries[id].Value["body"] != tc.filename {
				t.Fatalf("literal filename did not retain its commit: state=%+v err=%v", state, err)
			}
			if err := reopened.Close(ctx); err != nil {
				t.Fatal(err)
			}
			currentInfo, err := os.Stat(path)
			if err != nil || !os.SameFile(info, currentInfo) {
				t.Fatalf("database inode changed: %v", err)
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				if file.Name() != tc.filename && file.Name() != tc.filename+"-wal" && file.Name() != tc.filename+"-shm" {
					t.Fatalf("SQLite opened an alternate filename: %q", file.Name())
				}
			}
		})
	}
}

func TestMainSQLitePureGoConnectionPragmas(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "settings.sqlite")
	for pass := 0; pass < 2; pass++ {
		store := mainPureGoSQLiteOpen(t, path)
		// TEMP constraints query the adapter's own live connection. An external
		// connection cannot establish its synchronous or busy_timeout settings.
		// Use exec rather than depending on the connection field's representation.
		if err := store.exec(`CREATE TEMP TABLE purego_adapter_settings (
			mode TEXT NOT NULL CHECK(mode='wal'),
			sync INTEGER NOT NULL CHECK(sync=2),
			timeout INTEGER NOT NULL CHECK(timeout=5000))`); err != nil {
			t.Fatal(err)
		}
		check := func() {
			t.Helper()
			if err := store.exec(`INSERT INTO purego_adapter_settings VALUES (
				(SELECT journal_mode FROM pragma_journal_mode),
				(SELECT synchronous FROM pragma_synchronous),
				(SELECT timeout FROM pragma_busy_timeout))`); err != nil {
				t.Fatalf("open pass %d: require WAL, FULL (2), busy_timeout=5000: %v", pass, err)
			}
		}
		check()
		if err := store.Reclaim(ctx); err != nil {
			t.Fatal(err)
		}
		check()
		if err := store.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMainSQLitePureGoV140SchemaReplay(t *testing.T) {
	// Literal native v1.4 schema/JSON, not a fixture generated by the current
	// adapter, frame encoder or checkpoint encoder. maxPage=7 is persisted policy.
	const config = `{"version":1,"sync":true,"limits":{"maxFramePayloadBytes":4194304,"maxRecordBytes":524288,"maxDocumentBytes":262144,"maxStringBytes":262144,"maxRequestIDBytes":512,"maxDepth":32,"maxMembers":4096,"maxNodes":65536,"maxWrites":256,"maxPage":7,"maxRecords":100000,"maxRetainedBytes":33554432,"maxJournalBytes":268435456}}`
	const commit = `{"seq":1,"writes":[{"op":"append-entry","entry":{"id":2,"conversation":1,"kind":"fixture","value":{"body":"v1.4 α"}}},{"op":"append-entry","entry":{"id":3,"conversation":1,"kind":"fixture","value":{"body":"v1.4 β"}}}]}`
	const checkpoint = `{"seq":1,"highWater":3,"ordinal":3,"conversations":[{"id":1}],"entries":[{"id":2,"conversation":1,"kind":"fixture","value":{"body":"v1.4 α"},"seq":1,"position":1},{"id":3,"conversation":1,"kind":"fixture","value":{"body":"v1.4 β"},"seq":1,"position":2}],"tasks":[],"submissions":[],"documents":[],"revisions":[]}`
	for _, mode := range []string{"frames", "checkpoint-and-tail"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "v1.4.sqlite")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			db := mainPureGoSQLiteExternal(t, path)
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			for _, ddl := range []string{
				`CREATE TABLE config(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL)`,
				`CREATE TABLE frames(ordinal INTEGER PRIMARY KEY, kind INTEGER NOT NULL, highwater INTEGER NOT NULL, payload BLOB NOT NULL)`,
				`CREATE TABLE checkpoint(id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL)`,
			} {
				if _, err := tx.ExecContext(ctx, ddl); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO config(id,value) VALUES(1,?)", []byte(config)); err != nil {
				t.Fatal(err)
			}
			insertFrame := func(ordinal, kind, high int, payload string) {
				t.Helper()
				if _, err := tx.ExecContext(ctx, "INSERT INTO frames(ordinal,kind,highwater,payload) VALUES(?,?,?,?)", ordinal, kind, high, []byte(payload)); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "frames" {
				insertFrame(2, 1, 3, `{"first":2,"last":3}`)
				insertFrame(3, 2, 3, commit)
			} else if _, err := tx.ExecContext(ctx, "INSERT INTO checkpoint(id,value) VALUES(1,?)", []byte(checkpoint)); err != nil {
				t.Fatal(err)
			}
			// Reservation without a commit must survive migration and not be reused.
			insertFrame(4, 1, 4, `{"first":4,"last":4}`)
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			check := func(store *SQLiteStorage, seq, high uint64, count int) {
				t.Helper()
				limits, err := store.Limits()
				if err != nil || limits.MaxPage != 7 {
					t.Fatalf("persisted v1.4 policy lost: %+v err=%v", limits, err)
				}
				state, err := store.Snapshot(ctx)
				if err != nil || state.Seq != seq || state.HighWater != high || len(state.Entries) != count || len(state.Conversations) != 1 || state.Conversations[1].ID != 1 {
					t.Fatalf("v1.4 recovery state=%+v err=%v", state, err)
				}
				for i, body := range []string{"v1.4 α", "v1.4 β"} {
					entry := state.Entries[ID(i+2)]
					if entry.ID != ID(i+2) || entry.Conversation != 1 || entry.Kind != "fixture" || entry.Seq != 1 || entry.Position != uint64(i+1) || entry.Value["body"] != body {
						t.Fatalf("legacy entry changed: %+v", entry)
					}
				}
			}
			store := mainPureGoSQLiteOpen(t, path)
			check(store, 1, 4, 2)
			id, err := store.MintID(ctx)
			if err != nil || id != 5 {
				t.Fatalf("legacy reservation reused: id=%d err=%v", id, err)
			}
			if seq, err := store.Apply(ctx, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "fixture", Value: JSON{"body": "after migration"}}}}}); err != nil || seq != 2 {
				t.Fatalf("append after legacy replay: seq=%d err=%v", seq, err)
			}
			if err := store.Close(ctx); err != nil {
				t.Fatal(err)
			}
			reopened := mainPureGoSQLiteOpen(t, path)
			check(reopened, 2, 5, 3)
			state, err := reopened.Snapshot(ctx)
			if err != nil || state.Entries[id].Value["body"] != "after migration" {
				t.Fatalf("new commit lost on reopen: %+v err=%v", state.Entries[id], err)
			}
			if err := reopened.Close(ctx); err != nil {
				t.Fatal(err)
			}
			// Independent SQL inspection also catches reset physical ordinals.
			db = mainPureGoSQLiteExternal(t, path)
			var ordinal, kind, high int
			var payloadType string
			if err := db.QueryRowContext(ctx, "SELECT ordinal,kind,highwater,typeof(payload) FROM frames ORDER BY ordinal DESC LIMIT 1").Scan(&ordinal, &kind, &high, &payloadType); err != nil {
				t.Fatal(err)
			}
			if ordinal != 6 || kind != 2 || high != 5 || payloadType != "blob" {
				t.Fatalf("native frame layout changed: ordinal=%d kind=%d high=%d payload=%s", ordinal, kind, high, payloadType)
			}
		})
	}
}

// Witness Done evaluation inside enter/Close, not merely goroutine launch. This
// is a per-call admission witness; it never gates or replaces any SQL operation.
type mainPureGoSQLiteWaitContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *mainPureGoSQLiteWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func TestMainSQLitePureGoContentionAdmittedCancellationAndCloseOverlap(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "cancel"
		wantWaitErr := error(context.Canceled)
		if deadline {
			name, wantWaitErr = "deadline", context.DeadlineExceeded
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "contended.sqlite")
			s := mainPureGoSQLiteOpen(t, path)
			id, err := s.MintID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var timeout int
			if err := s.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 5000 {
				t.Fatalf("require unchanged busy_timeout=5000: got %d err=%v", timeout, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(filepath.Dir(path), "same-inode.sqlite")
			if err := os.Link(path, alias); err != nil {
				t.Fatal(err)
			}
			// This connection deliberately bypasses native ownership via the real
			// driver, NOT nolock/immutable/another URI-selected database.
			external := mainPureGoSQLiteExternal(t, path)
			locker, err := external.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			transactionOpen := false
			t.Cleanup(func() {
				// Release the external lock before native cleanup joins the writer,
				// including assertion failures before the explicit COMMIT below.
				if transactionOpen {
					if _, err := locker.ExecContext(ctx, "ROLLBACK"); err != nil {
						t.Error("external rollback cleanup:", err)
					}
				}
				if err := locker.Close(); err != nil {
					t.Error("external connection cleanup:", err)
				}
			})
			var schema, filename string
			var databaseIndex int
			if err := locker.QueryRowContext(ctx, "PRAGMA database_list").Scan(&databaseIndex, &schema, &filename); err != nil {
				t.Fatal(err)
			}
			externalInfo, err := os.Stat(filename)
			if err != nil || schema != "main" || !os.SameFile(info, externalInfo) {
				t.Fatalf("external driver did not open the owned inode: schema=%q file=%q err=%v", schema, filename, err)
			}
			if _, err := locker.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
				t.Fatal(err)
			}
			transactionOpen = true
			lockStarted := time.Now()
			// A real write in the external transaction confirms SQLite's writer
			// lock, independently of the storage flock/path ownership fence.
			result, err := locker.ExecContext(ctx, "UPDATE config SET value=value WHERE id=1")
			if err != nil {
				t.Fatal(err)
			}
			if rows, err := result.RowsAffected(); err != nil || rows != 1 {
				t.Fatalf("external write-lock fixture: rows=%d err=%v", rows, err)
			}
			await := func(phase string, done <-chan struct{}) {
				t.Helper()
				timer := time.NewTimer(time.Second)
				defer timer.Stop()
				select {
				case <-done:
				case <-timer.C:
					t.Fatalf("%s did not reach its witnessed phase", phase)
				}
			}
			newWaitContext := func() (*mainPureGoSQLiteWaitContext, context.CancelFunc) {
				var waitCtx context.Context
				var cancel context.CancelFunc
				if deadline {
					waitCtx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
				} else {
					waitCtx, cancel = context.WithCancel(ctx)
				}
				return &mainPureGoSQLiteWaitContext{Context: waitCtx, observed: make(chan struct{})}, cancel
			}
			entered := make(chan struct{})
			appendFrame := s.appendFrame
			s.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				close(entered) // Apply owns storeCore.line and has prepared its frame.
				// No test release gate: only external COMMIT can unblock this SQL.
				return appendFrame(kind, ordinal, high, payload)
			}
			caller, cancelCaller := context.WithCancel(ctx)
			defer cancelCaller()
			type applyResult struct {
				seq uint64
				err error
			}
			applied := make(chan applyResult, 1)
			go func() {
				seq, err := s.Apply(caller, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "fixture", Value: JSON{"body": "settled after caller cancellation"}}}}})
				applied <- applyResult{seq, err}
			}()
			await("admitted frame append", entered)
			assertBlocked := func() {
				t.Helper()
				select {
				case result := <-applied:
					t.Fatalf("admitted Apply completed before external COMMIT: %+v", result)
				default:
				}
				select {
				case <-s.closeDone:
					t.Fatal("Close finished before external COMMIT")
				default:
				}
				if len(s.line) != 0 {
					t.Fatal("admitted writer released storeCore.line before SQL settlement")
				}
				var frames, commits int
				if err := locker.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(kind=2),0) FROM frames").Scan(&frames, &commits); err != nil || frames != 2 || commits != 1 {
					t.Fatalf("external locked transaction observed premature frames: frames=%d commits=%d err=%v", frames, commits, err)
				}
			}
			cancelCaller()
			assertBlocked()

			readCtx, cancelRead := newWaitContext()
			defer cancelRead()
			readStarted := time.Now()
			readDone := make(chan struct{})
			var state Snapshot
			var readErr error
			go func() {
				state, readErr = s.Snapshot(readCtx)
				close(readDone)
			}()
			await("queued Snapshot admission", readCtx.observed)
			if !deadline {
				cancelRead()
			}
			await("queued Snapshot cancellation", readDone)
			readElapsed := time.Since(readStarted)
			if !errors.Is(readErr, wantWaitErr) || state.Seq != 0 || len(state.Entries) != 0 || readElapsed >= time.Second {
				t.Fatalf("queued read cancellation: state=%+v err=%v elapsed=%s want=%v within 1s", state, readErr, readElapsed, wantWaitErr)
			}
			assertBlocked()

			closeCtx, cancelClose := newWaitContext()
			defer cancelClose()
			closeStarted := time.Now()
			closeReturned := make(chan struct{})
			var closeErr error
			go func() { closeErr = s.Close(closeCtx); close(closeReturned) }()
			await("Close sealed and waiting", closeCtx.observed)
			// A second Close joins the same background drain while the first
			// caller cancels. Its witnessed wait also precedes external COMMIT.
			joinCtx := &mainPureGoSQLiteWaitContext{Context: ctx, observed: make(chan struct{})}
			joined := make(chan struct{})
			var joinErr error
			go func() { joinErr = s.Close(joinCtx); close(joined) }()
			await("background Close join", joinCtx.observed)
			if !deadline {
				cancelClose()
			}
			await("Close waiter cancellation", closeReturned)
			closeElapsed := time.Since(closeStarted)
			if !errors.Is(closeErr, wantWaitErr) || closeElapsed >= time.Second || !s.closing.Load() {
				t.Fatalf("Close cancellation must leave admission sealed: err=%v elapsed=%s sealed=%v want=%v within 1s", closeErr, closeElapsed, s.closing.Load(), wantWaitErr)
			}
			if _, err := s.Snapshot(ctx); !errors.Is(err, ErrClosed) {
				t.Fatalf("late read bypassed Close seal: %v", err)
			}
			if _, err := s.Apply(ctx, Batch{}); !errors.Is(err, ErrClosed) {
				t.Fatalf("late write bypassed Close seal: %v", err)
			}
			successor, err := OpenSQLite(alias, SQLiteOptions{})
			if successor != nil {
				_ = successor.Close(ctx)
			}
			if !errors.Is(err, ErrOwned) || successor != nil {
				t.Fatalf("cancelled Close released the inode lock before external COMMIT: %v", err)
			}
			assertBlocked()
			select {
			case <-joined:
				t.Fatalf("background Close escaped admitted SQL: %v", joinErr)
			default:
			}

			if _, err := locker.ExecContext(ctx, "COMMIT"); err != nil {
				t.Fatal(err)
			}
			transactionOpen = false
			lockElapsed := time.Since(lockStarted)
			settleStarted := time.Now()
			await("background Close after SQL release", joined)
			if joinErr != nil {
				t.Fatal("background Close:", joinErr)
			}
			// Close can finish just before the Apply goroutine sends its result;
			// await that result separately rather than assuming scheduler order.
			var settled applyResult
			select {
			case settled = <-applied:
			case <-time.After(time.Second):
				t.Fatal("admitted Apply did not return after SQL release")
			}
			if settled.err != nil || settled.seq != 2 || !errors.Is(caller.Err(), context.Canceled) {
				t.Fatalf("admitted caller cancellation changed SQL settlement: result=%+v caller=%v", settled, caller.Err())
			}
			if s.pool.Stats().OpenConnections != 0 {
				t.Fatal("Close returned before physically closing the native SQL connection")
			}
			var frames, commits int
			if err := locker.QueryRowContext(ctx, "SELECT count(*),coalesce(sum(kind=2),0) FROM frames").Scan(&frames, &commits); err != nil || frames != 3 || commits != 2 {
				t.Fatalf("independent SQL did not observe the settled write: frames=%d commits=%d err=%v", frames, commits, err)
			}
			reopened := mainPureGoSQLiteOpen(t, path)
			state, err = reopened.Snapshot(ctx)
			if err != nil || state.Seq != 2 || state.HighWater != uint64(id) || len(state.Entries) != 1 || state.Entries[id].Seq != 2 || state.Entries[id].Position != 1 || state.Entries[id].Value["body"] != "settled after caller cancellation" {
				t.Fatalf("reopen lost the admitted cancelled caller's write: state=%+v err=%v", state, err)
			}
			reopenedInfo, err := os.Stat(path)
			if err != nil || !os.SameFile(info, reopenedInfo) {
				t.Fatalf("reopen did not reacquire the original inode: %v", err)
			}
			t.Logf("busy_timeout=%dms; caller=%v; queued read=%v in %s; Close waiter=%v in %s; external writer lock held %s; release/drain/SQL recovery/reacquire=%s", timeout, caller.Err(), readErr, readElapsed, closeErr, closeElapsed, lockElapsed, time.Since(settleStarted))
		})
	}
}
