package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These tests translate the public, SQLite-representable parts of the five
// unchanged pi v1.7.0 Durable originals: sqlite-commit, sqlite-facade,
// sqlite-migrations, sqlite-storage and the SQLite arm of the runtime-boundary
// test. JavaScript driver-specific prepare counts and excluded backends are
// deliberately not modeled.

func TestV170SQLiteFacadePublicSettlementAndAllocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "facade.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	root := StorageWrite{Type: "conversation", Value: Conversation{ID: 1}}
	first := StorageWrite{Type: "entry", Value: Entry{ID: 2, Conversation: 1, Kind: "first"}}
	var receipts [][2]uint64
	result, err := store.Commit(bg, [][]StorageWrite{{root}, {first}}, func(index int, seq uint64) {
		receipts = append(receipts, [2]uint64{uint64(index), seq})
	})
	if err != nil || result.Stored != 2 || result.Refused != nil || !reflect.DeepEqual(receipts, [][2]uint64{{0, 1}, {1, 2}}) {
		t.Fatalf("settlement=%+v receipts=%v err=%v", result, receipts, err)
	}
	// A refused claim is not a transaction settlement and cannot advance the
	// allocator or displace the admitted prefix.
	result, err = store.Commit(bg, [][]StorageWrite{{
		{Type: "entry", Value: Entry{ID: 100, Conversation: 1, Kind: "not-stored"}},
		root,
	}}, nil)
	if err != nil || result.Stored != 0 || result.Refused == nil {
		t.Fatalf("refusal=%+v err=%v", result, err)
	}
	if id, err := store.MintID(bg); err != nil || id != 3 {
		t.Fatal(id, err)
	}
	if got, found, err := store.Entry(bg, 100); err != nil || found {
		t.Fatal(got, found, err)
	}

	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal("repeated close:", err)
	}
	if _, found, err := store.Entry(bg, 2); !errors.Is(err, ErrClosed) || found {
		t.Fatalf("closed read: found=%v err=%v", found, err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	if got, found, err := view.Entry(bg, 2); err != nil || !found || got.Entry.Kind != "first" || got.CommitSeq != 2 {
		t.Fatal(got, found, err)
	}
}

func TestV170SQLiteMigrationsPreserveDataAndRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.sqlite")
	createV170SQLiteVersionOne(t, path)

	// Force migration 2 to fail after its first statement and metadata setup.
	raw := openV170RawSQLite(t, path)
	if _, err := raw.Exec(`CREATE TABLE tasks_v2 (id INTEGER PRIMARY KEY) STRICT`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	failed, err := OpenSQLite(path, SQLiteOptions{})
	if failed != nil {
		failed.Close(bg)
	}
	if err == nil {
		t.Fatal("incompatible migration unexpectedly succeeded")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}

	// The failed migration leaves the v1 schema and its task untouched.
	raw = openV170RawSQLite(t, path)
	var version, taskCount, probeCount int
	if err := raw.QueryRow(`SELECT version FROM durable_schema WHERE singleton=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow(`SELECT count(*) FROM tasks WHERE id=5`).Scan(&taskCount); err != nil {
		t.Fatal(err)
	}
	if err := raw.QueryRow(`SELECT count(*) FROM tasks_v2`).Scan(&probeCount); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DROP TABLE tasks_v2`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if version != 1 || taskCount != 1 || probeCount != 0 {
		t.Fatalf("failed migration changed v1: version=%d task=%d probe=%d", version, taskCount, probeCount)
	}

	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if task, found, err := store.Task(bg, 5); err != nil || !found || task.Kind != "retained.task" || task.State.Status != "waiting" {
		t.Fatal(task, found, err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}

	// Reapplying current migrations is stable, and the v2 sleeping state is now
	// accepted without losing the source's indexes.
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	// The migration itself is database-schema behavior; match the original by
	// inserting sleeping through the migrated SQLite table.
	const sleeping = `{"id":6,"conversationId":1,"kind":"sleep.task","version":1,"input":null,"state":{"status":"sleeping","checkpoint":{"phase":"sleep"}},"background":false,"abortRequested":false}`
	if _, err := store.db.ExecContext(bg, `INSERT INTO record_ids(id,record_type) VALUES(6,'task')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(bg, `INSERT INTO tasks(id,conversation_id,kind,status,abort_requested,background,record) VALUES(6,1,'sleep.task','sleeping',0,0,?)`, sleeping); err != nil {
		t.Fatal(err)
	}
	var names string
	rows, err := store.db.QueryContext(bg, `SELECT group_concat(name, ',') FROM (SELECT name FROM sqlite_schema WHERE type='index' AND tbl_name='tasks' ORDER BY name)`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		if err := rows.Scan(&names); err != nil {
			t.Fatal(err)
		}
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tasks_by_abort_requested", "tasks_by_background", "tasks_by_conversation", "tasks_by_kind", "tasks_by_status"} {
		if !strings.Contains(names, name) {
			t.Fatalf("migration lost %s: %s", name, names)
		}
	}
}

func TestV170SQLiteStorageReopenAndCorruptionFence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "records.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	writes := []StorageWrite{
		{Type: "conversation", Value: Conversation{ID: 1}},
		{Type: "document.create", Record: &DocumentCreate{ID: 2, Kind: "corrupt", Scope: DocumentCreateScope{Kind: "session"}}, Content: &DocumentContent{Kind: "base", Version: 1, Value: JSON{"retained": true}}},
	}
	result, err := store.Commit(bg, [][]StorageWrite{writes}, nil)
	if err != nil || result.Stored != 1 || result.Refused != nil {
		t.Fatal(result, result.Refused, err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}

	raw := openV170RawSQLite(t, path)
	if _, err := raw.Exec(`DELETE FROM document_revisions WHERE document_id=2`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if store != nil {
		store.Close(bg)
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("missing base did not remain a corruption fence: %v", err)
	}
}

func TestV170SQLiteRejectsNewerSchemaBeforeMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newer.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	raw := openV170RawSQLite(t, path)
	if _, err := raw.Exec(`UPDATE durable_schema SET version=?`, upstreamSQLiteSchemaVersion+1); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if store != nil {
		store.Close(bg)
	}
	if err == nil || !strings.Contains(err.Error(), "newer than supported version") {
		t.Fatal(err)
	}
}

func createV170SQLiteVersionOne(t *testing.T, path string) {
	t.Helper()
	raw := openV170RawSQLite(t, path)
	var migrations []struct {
		Version    int      `json:"version"`
		Statements []string `json:"statements"`
	}
	if err := json.Unmarshal([]byte(upstreamSQLiteSchema), &migrations); err != nil {
		t.Fatal(err)
	}
	statements := append([]string{
		`CREATE TABLE durable_schema (singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL CHECK(version >= 0)) STRICT`,
		`INSERT INTO durable_schema(singleton,version) VALUES(1,0)`,
	}, migrations[0].Statements...)
	for _, statement := range statements {
		if _, err := raw.Exec(strings.ReplaceAll(statement, "pi_schema_", "")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := raw.Exec(`UPDATE durable_schema SET version=1`); err != nil {
		t.Fatal(err)
	}
	task := `{"id":5,"conversationId":1,"kind":"retained.task","version":1,"input":null,"background":false,"abortRequested":false,"state":{"status":"waiting","checkpoint":{"phase":"run"},"on":[],"policy":"allSettled"}}`
	if _, err := raw.Exec(`INSERT INTO record_ids(id,record_type) VALUES(5,'task')`); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO tasks(id,conversation_id,kind,status,abort_requested,background,record) VALUES(5,1,'retained.task','waiting',0,0,?)`, task); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`UPDATE durable_metadata SET next_id='6'`); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
}

func openV170RawSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	connector, err := newSQLiteConnector("file:" + filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}
