package durable

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSourceV152SQLiteViewerReadsUpstreamProducedDatabase(t *testing.T) {
	bytesBefore, err := os.ReadFile("testdata/pi-durable-v152/viewer.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, bytesBefore, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("read upstream-produced SQLite: %v", err)
	}
	defer store.Close(bg)
	conv, found, err := store.Conversation(bg, 1)
	if err != nil || !found || conv.ID != 1 {
		t.Fatal(conv, found, err)
	}
	entries, err := store.ScanEntries(bg, EntryQuery{Conversation: 1}, 10, nil)
	if err != nil || len(entries.Items) != 2 || entries.Items[0].ID != 7 || entries.Items[1].ID != 3 {
		t.Fatal(entries, err)
	}
	entry, found, err := store.Entry(bg, 1, 3)
	if err != nil || !found || entry.CommitSeq != 1 || entry.Entry.Data != "first" {
		t.Fatal(entry, found, err)
	}
	head, found, err := store.FindLatestHeadMarker(bg, 1, 7)
	if err != nil || !found || head.ID != 7 || head.Head != 7 || !equalJSONValue(head.Data, JSON{"done": true}) {
		t.Fatal(head, found, err)
	}
	task, found, err := store.Task(bg, 4)
	if err != nil || !found {
		t.Fatal(task, found, err)
	}
	if got, err := json.Marshal(task); err != nil || !jsonValuesEqual(got, []byte(`{"id":4,"conversationId":1,"kind":"test.task","version":1,"input":{"x":2},"background":false,"abortRequested":false,"state":{"status":"running","checkpoint":{"phase":"run"}}}`)) {
		t.Fatal(string(got), err)
	}
	submission, found, err := store.SubmissionByRequest(bg, 1, "request")
	if err != nil || !found || submission.ID != 5 || submission.Status != "placed" || submission.Entry != 3 {
		t.Fatal(submission, found, err)
	}
	for seq, text := range map[uint64]string{1: "before", 2: "after"} {
		point := DocumentPoint{Seq: seq}
		addr := DocumentAddress{Scope: "conversation", Owner: 1, Kind: "test.document"}
		record, found, err := store.FindDocument(bg, addr, point)
		if err != nil || !found || record.ID != 6 || record.CreatedAt != 1 {
			t.Fatal(record, found, err)
		}
		content, found, err := store.DocumentAt(bg, record.ID, point)
		if err != nil || !found || content.Version != 1 || !equalJSONValue(content.Value, JSON{"text": text}) {
			t.Fatal(content, found, err)
		}
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	bytesAfter, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(bytesBefore, bytesAfter) {
		t.Fatal("upstream database changed", err)
	}
}

func jsonValuesEqual(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && equalJSONValue(x, y)
}

func TestSourceV152SQLiteCommitsIntoUpstreamProducedDatabase(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v152/viewer.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatalf("open original schema for writing: %v", err)
	}
	defer store.Close(bg)
	id, err := store.MintID(bg)
	if err != nil || id != 8 {
		t.Fatal(id, err)
	}
	task, found, err := store.Task(bg, 4)
	if err != nil || !found {
		t.Fatal(task, found, err)
	}
	task.State.Status = "pending"
	ready := []StorageWrite{
		{Type: "entry", Value: Entry{ID: id, Conversation: 1, Kind: "note", Data: "go", HasData: true}},
		{Type: "task", Value: task},
		{Type: "submission", Value: SubmissionRecord{ID: 5, Conversation: 1, RequestID: "request", Type: "input", Status: "done", Entry: 3, Answer: id}},
		{Type: "document.change", ID: 6, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"text"}, "written"}}}},
	}
	var receipts []publicSQLiteReceipt
	result, err := store.Commit(bg, [][]StorageWrite{ready, {{Type: "entry", Value: Entry{ID: id, Conversation: 1, Kind: "duplicate"}}}, {{Type: "entry", Value: Entry{ID: 9, Conversation: 1, Kind: "not-stored"}}}}, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
	if err != nil || result.Stored != 1 || result.Refused == nil || !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 3}}) {
		t.Fatal(result, receipts, err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	entry, found, err := view.Entry(bg, id)
	if err != nil || !found || entry.CommitSeq != 3 || entry.Entry.Data != "go" {
		t.Fatal(entry, found, err)
	}
	if _, found, err := view.Entry(bg, 9); err != nil || found {
		t.Fatal(found, err)
	}
	readTask, found, err := view.Task(bg, 4)
	if err != nil || !found || readTask.State.Status != "pending" {
		t.Fatal(readTask, found, err)
	}
	submission, found, err := view.SubmissionByRequest(bg, 1, "request")
	if err != nil || !found || submission.Status != "done" || submission.Answer != id {
		t.Fatal(submission, found, err)
	}
	for seq, text := range map[uint64]string{2: "after", 3: "written"} {
		d, found, err := view.DocumentAt(bg, 6, DocumentPoint{Seq: seq})
		if err != nil || !found || !equalJSONValue(d.Value, JSON{"text": text}) {
			t.Fatal(d, found, err)
		}
	}
	var nativeTouched int
	if err := view.db.QueryRowContext(bg, "SELECT count(*) FROM sqlite_schema WHERE name IN ('config','frames','checkpoint')").Scan(&nativeTouched); err != nil || nativeTouched != 0 {
		t.Fatal(nativeTouched, err)
	}
}

func TestSourceV152SQLiteMigratesVersionOneOnlyForWriter(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v152/schema-v1.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	version := func(s *SQLiteStorage) int {
		t.Helper()
		var v int
		if err := s.db.QueryRowContext(bg, "SELECT version FROM durable_schema WHERE singleton=1").Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if v := version(view); v != 1 {
		t.Fatal("viewer migrated the file", v)
	}
	if err := view.Close(bg); err != nil {
		t.Fatal(err)
	}
	writer, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	if v := version(writer); v != 2 {
		t.Fatalf("writable open left version %d; want 2", v)
	}
	task, found, err := writer.Task(bg, 2)
	if err != nil || !found || task.State.Status != "running" {
		t.Fatal(task, found, err)
	}
	until := int64(9000)
	task.State.Status, task.State.Until = "sleeping", &until
	result, err := writer.Commit(bg, [][]StorageWrite{{{Type: "task", Value: task}}}, nil)
	if err != nil || result.Stored != 1 || result.Refused != nil {
		t.Fatal(result, err)
	}
	if err := writer.Close(bg); err != nil {
		t.Fatal(err)
	}
	view, err = OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	task, found, err = view.Task(bg, 2)
	if err != nil || !found || task.State.Status != "sleeping" || task.State.Until == nil || *task.State.Until != until {
		t.Fatal(task, found, err)
	}
}

func TestSourceV152UpstreamSQLiteWriteFailureRollsBackAllAcceptedParts(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v152/viewer.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	writer, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	if err := writer.exec("CREATE TRIGGER fail_original BEFORE INSERT ON entries WHEN NEW.id=9 BEGIN SELECT RAISE(ABORT,'test'); END"); err != nil {
		t.Fatal(err)
	}
	batches := [][]StorageWrite{{{Type: "entry", Value: Entry{ID: 8, Conversation: 1, Kind: "first"}}}, {{Type: "entry", Value: Entry{ID: 9, Conversation: 1, Kind: "second"}}}}
	var receipts []publicSQLiteReceipt
	result, err := writer.Commit(bg, batches, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
	var rejection *StorageRejected
	if !errors.As(err, &rejection) || errors.Is(err, ErrPoisoned) || result.Stored != 0 || len(receipts) != 0 {
		t.Fatal(result, receipts, err)
	}
	if err := writer.Close(bg); err != nil {
		t.Fatal(err)
	}
	writer, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	if _, found, err := writer.Entry(bg, 8); err != nil || found {
		t.Fatal(found, err)
	}
	if err := writer.exec("DROP TRIGGER fail_original"); err != nil {
		t.Fatal(err)
	}
	result, err = writer.Commit(bg, batches, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
	if err != nil || result.Stored != 2 || result.Refused != nil || !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 3}, {1, 4}}) {
		t.Fatal(result, receipts, err)
	}
}

// Statement counts are measured on the original table layout as well as the
// native frame layout. A group shares one SQL plan, whatever its length.
func TestSourceV152UpstreamSQLiteGroupReusesSQL(t *testing.T) {
	var baseline []string
	for _, count := range []int{1, 3, 150} {
		data, err := os.ReadFile("testdata/pi-durable-v152/viewer.sqlite")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "session.sqlite")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close(bg) })
		setup := make([]StorageWrite, count)
		batches := make([][]StorageWrite, count)
		for i := range count {
			id := ID(100 + i)
			key := fmt.Sprint(id)
			setup[i] = v152SessionDocument(id, "counter", &key, JSON{"n": 0})
			batches[i] = []StorageWrite{
				{Type: "document.change", ID: id, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"n"}, 1}}}},
				{Type: "entry", Value: Entry{ID: ID(400 + i), Conversation: 1, Kind: "note", HasData: true, Data: 0}},
			}
		}
		if result, err := store.Commit(bg, [][]StorageWrite{setup}, nil); err != nil || result.Refused != nil || result.Stored != 1 {
			t.Fatal(result, err)
		}
		capture := &recordingSQLiteConnection{sqliteConnection: store.db}
		store.db = capture
		var receipts []publicSQLiteReceipt
		result, err := store.Commit(bg, batches, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
		if err != nil || result.Refused != nil || result.Stored != count || len(receipts) != count {
			t.Fatal(result, len(receipts), err)
		}
		for i, receipt := range receipts {
			if receipt != (publicSQLiteReceipt{i, uint64(4 + i)}) {
				t.Fatal(receipt)
			}
		}
		var statements []string
		for _, call := range capture.calls {
			statements = append(statements, call.text)
			if strings.Contains(call.text, "json_each") {
				checkCompactFramePlan(t, capture.sqliteConnection, call)
			}
		}
		if count == 1 {
			baseline = statements
		}
		if len(statements) != 7 || !reflect.DeepEqual(statements, baseline) {
			t.Errorf("canonical group of %d used %d statements; want 7 with stable SQL", count, len(statements))
		}
		if err := store.Close(bg); err != nil {
			t.Fatal(err)
		}
		view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		for i := range count {
			e, found, err := view.Entry(bg, ID(400+i))
			if err != nil || !found || e.CommitSeq != uint64(4+i) || !equalJSONValue(e.Entry.Data, 0) {
				t.Fatal(e, found, err)
			}
			d, found, err := view.DocumentAt(bg, ID(100+i), CurrentDocumentPoint())
			if err != nil || !found || !equalJSONValue(d.Value, JSON{"n": 1}) {
				t.Fatal(d, found, err)
			}
		}
		if err := view.Close(bg); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSourceV152UpstreamSQLiteCurrentOnlyGroupPrunesBeforeNewBase(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v152/viewer.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	delta := func(n int) StorageWrite {
		return StorageWrite{Type: "document.change", ID: 10, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"n"}, n}}}}
	}
	batches := [][]StorageWrite{
		{v152SessionDocument(10, "current", nil, JSON{"n": 0})},
		{delta(1)},
		{{Type: "document.change", ID: 10, Content: &DocumentContent{Kind: "base", Version: 1, Value: JSON{"n": 2}}}},
		{delta(3)},
	}
	result, err := store.Commit(bg, batches, nil)
	if err != nil || result.Refused != nil || result.Stored != 4 {
		t.Fatal(result, err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	doc, found, err := view.DocumentAt(bg, 10, CurrentDocumentPoint())
	if err != nil || !found || !equalJSONValue(doc.Value, JSON{"n": 3}) || doc.DeltasSinceBase != 1 {
		t.Fatal(doc, found, err)
	}
	rows, err := view.db.QueryContext(bg, "SELECT seq,kind FROM document_revisions WHERE document_id=10 ORDER BY seq")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var revisions []string
	for rows.Next() {
		var seq uint64
		var kind string
		if err := rows.Scan(&seq, &kind); err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, fmt.Sprintf("%d:%s", seq, kind))
	}
	if rows.Err() != nil || !reflect.DeepEqual(revisions, []string{"5:base", "6:delta"}) {
		t.Fatal(revisions, rows.Err())
	}
}

func TestSourceV152UpstreamSQLiteMintPersistsOnlyWithAcceptedCommit(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v152/viewer.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	open := func() *SQLiteStorage {
		t.Helper()
		s, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close(bg) })
		return s
	}
	metadata := func(s *SQLiteStorage, id string, seq int) {
		t.Helper()
		var nextId string
		var nextSeq int
		if err := s.db.QueryRowContext(bg, "SELECT next_id,next_seq FROM durable_metadata WHERE singleton=1").Scan(&nextId, &nextSeq); err != nil || nextId != id || nextSeq != seq {
			t.Fatalf("metadata = %s/%d, %v; want %s/%d", nextId, nextSeq, err, id, seq)
		}
	}
	store := open()
	if id, err := store.MintID(bg); err != nil || id != 8 {
		t.Fatal(id, err)
	}
	metadata(store, "8", 3)
	refused, err := store.Commit(bg, [][]StorageWrite{{{Type: "entry", Value: Entry{ID: 3, Conversation: 1, Kind: "duplicate"}}}}, nil)
	if err != nil || refused.Refused == nil || refused.Stored != 0 {
		t.Fatal(refused, err)
	}
	metadata(store, "8", 3)
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store = open()
	for _, want := range []ID{8, 9} {
		if id, err := store.MintID(bg); err != nil || id != want {
			t.Fatal(id, err, want)
		}
	}
	accepted, err := store.Commit(bg, [][]StorageWrite{{{Type: "entry", Value: Entry{ID: 8, Conversation: 1, Kind: "committed"}}}}, nil)
	if err != nil || accepted.Refused != nil || accepted.Stored != 1 {
		t.Fatal(accepted, err)
	}
	metadata(store, "10", 4) // includes the other candidate minted during this open
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store = open()
	if id, err := store.MintID(bg); err != nil || id != 10 {
		t.Fatal(id, err)
	}
}

func writeSQLiteMigrationFixture(t *testing.T, path string, statements ...string) {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: path}
	connector, err := newSQLiteConnector(uri.String())
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.ExecContext(bg, statement); err != nil {
			t.Fatal(err)
		}
	}
}

func copySQLiteV152Fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "pi-durable-v152", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// sqlite-migrations.test.ts: an unsupported version rejects through the
// storage opener, rather than applying any older migrations.
func TestSourceV152SQLiteRejectsNewerSchema(t *testing.T) {
	path := copySQLiteV152Fixture(t, "viewer.sqlite")
	writeSQLiteMigrationFixture(t, path, "UPDATE durable_schema SET version=3 WHERE singleton=1")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if store != nil {
		_ = store.Close(bg)
	}
	if store != nil || err == nil || err.Error() != "SQLite schema version 3 is newer than supported version 2" {
		t.Fatalf("future schema: storage %p, error %v", store, err)
	}
}

// Supplemental: inject a late DDL failure into the real v1→v2 migration.
// Failing after the DROP/ALTER must restore the original version and rows.
func TestSourceV152SQLiteMigrationFailureCanRetry(t *testing.T) {
	path := copySQLiteV152Fixture(t, "schema-v1.sqlite")
	writeSQLiteMigrationFixture(t, path,
		"DROP INDEX tasks_by_background",
		"CREATE INDEX tasks_by_background ON conversations(id)")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if store != nil {
		_ = store.Close(bg)
	}
	if store != nil || err == nil || !strings.Contains(err.Error(), "index tasks_by_background already exists") {
		t.Fatalf("migration did not reach the injected failure: storage %p, error %v", store, err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	var version, leftover int
	if err := view.db.QueryRowContext(bg, "SELECT version FROM durable_schema WHERE singleton=1").Scan(&version); err != nil || version != 1 {
		t.Fatal(version, err)
	}
	if err := view.db.QueryRowContext(bg, "SELECT count(*) FROM sqlite_schema WHERE name='tasks_v2'").Scan(&leftover); err != nil || leftover != 0 {
		t.Fatal(leftover, err)
	}
	original, found, err := view.Task(bg, 2)
	if err != nil || !found || original.State.Status != "running" {
		t.Fatal(original, found, err)
	}
	writeSQLiteMigrationFixture(t, path, "DROP INDEX tasks_by_background")
	writer, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	if err := writer.db.QueryRowContext(bg, "SELECT version FROM durable_schema WHERE singleton=1").Scan(&version); err != nil || version != 2 {
		t.Fatal(version, err)
	}
	task, found, err := writer.Task(bg, 2)
	if err != nil || !found || !reflect.DeepEqual(task, original) {
		t.Fatal(task, original, found, err)
	}
}

// A schema metadata row at version zero is the initialization state of the
// source migration runner; all migration statements must run on writer open.
func TestSourceV152SQLiteMigratesVersionZeroWithPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	const prefix = "unit_"
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	writeSQLiteMigrationFixture(t, path,
		"CREATE TABLE unit_durable_schema (singleton INTEGER PRIMARY KEY CHECK (singleton = 1), version INTEGER NOT NULL CHECK (version >= 0)) STRICT",
		"INSERT INTO unit_durable_schema (singleton, version) VALUES (1, 0)")
	writer, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	var version int
	if err := writer.db.QueryRowContext(bg, "SELECT version FROM unit_durable_schema WHERE singleton=1").Scan(&version); err != nil || version != 2 {
		t.Fatal(version, err)
	}
	var id string
	var seq uint64
	if err := writer.db.QueryRowContext(bg, "SELECT next_id,next_seq FROM unit_durable_metadata WHERE singleton=1").Scan(&id, &seq); err != nil || id != "2" || seq != 1 {
		t.Fatal(id, seq, err)
	}
	result, err := writer.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}, {Type: "entry", Value: Entry{ID: 2, Conversation: 1, Kind: "retained", HasData: true, Data: JSON{"retained": true}}}}}, nil)
	if err != nil || result.Stored != 1 || result.Refused != nil {
		t.Fatal(result, err)
	}
	if err := writer.Close(bg); err != nil {
		t.Fatal(err)
	}
	writer, err = OpenSQLite(path, SQLiteOptions{Prefix: prefix})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	entry, ok, err := writer.Entry(bg, 2)
	if err != nil || !ok || entry.CommitSeq != 1 || !reflect.DeepEqual(entry.Entry.Data, map[string]any{"retained": true}) {
		t.Fatal(entry, ok, err)
	}
}

// store-storage-commit.test.ts, "refuses a later batch with ...": staging
// diagnoses the malformed part before dereferencing its missing document.
func TestSourceV152SQLiteDuplicateDocumentCommandsRefuseAfterPrefix(t *testing.T) {
	const doc ID = 101
	content := &DocumentContent{Kind: "base", Version: 1, Value: JSON{}}
	for _, tc := range []struct {
		name    string
		command StorageWrite
		want    string
	}{
		{"retirement", StorageWrite{Type: "document.retire", ID: doc}, "Document 101 is retired more than once"},
		{"content", StorageWrite{Type: "document.change", ID: doc, Content: content}, "Document 101 has more than one content command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := OpenSQLite(copySQLiteV152Fixture(t, "viewer.sqlite"), SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close(bg)
			var receipts []publicSQLiteReceipt
			result, err := store.Commit(bg, [][]StorageWrite{
				{{Type: "conversation", Value: Conversation{ID: 100}}},
				{tc.command, tc.command},
			}, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
			var refusal *StorageRejected
			if err != nil || result.Stored != 1 || !errors.As(result.Refused.AsError(), &refusal) || refusal.Reason != tc.want {
				t.Fatalf("commit stored %d, refusal %v, error %v; want one stored and %q", result.Stored, result.Refused.AsError(), err, tc.want)
			}
			if !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 3}}) {
				t.Fatal(receipts)
			}
			conversation, found, err := store.Conversation(bg, 100)
			if err != nil || !found || conversation != (Conversation{ID: 100}) {
				t.Fatal(conversation, found, err)
			}
		})
	}
}

// sqlite-storage.test.ts, "reclaims current-only revisions only after a base
// or retirement", plus reopening the real Harness over the pruned database.
func TestSourceV152SQLitePrunedDocumentAllowsHarnessReopen(t *testing.T) {
	path := copySQLiteV152Fixture(t, "viewer.sqlite")
	writer, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	id, err := writer.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	commit := func(write StorageWrite) {
		t.Helper()
		result, err := writer.Commit(bg, [][]StorageWrite{{write}}, nil)
		if err != nil || result.Stored != 1 || result.Refused != nil {
			t.Fatal(result, err)
		}
	}
	checkCount := func(want int) {
		t.Helper()
		var count int
		if err := writer.db.QueryRowContext(bg, "SELECT count(*) FROM document_revisions WHERE document_id=?", id).Scan(&count); err != nil || count != want {
			t.Fatalf("revision count = %d, %v; want %d", count, err, want)
		}
	}
	change := func(content *DocumentContent) {
		commit(StorageWrite{Type: "document.change", ID: id, Content: content})
	}
	commit(v152SessionDocument(id, "latest", nil, JSON{"count": 0}))
	for i := 1; i <= 10; i++ {
		change(&DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"count"}, i}}})
	}
	checkCount(11)
	var latest string
	if err := writer.db.QueryRowContext(bg, "SELECT content FROM document_revisions WHERE document_id=? AND kind='delta' ORDER BY seq DESC LIMIT 1", id).Scan(&latest); err != nil || latest != `[["s",["count"],10]]` {
		t.Fatal(latest, err)
	}
	change(&DocumentContent{Kind: "base", Version: 1, Value: JSON{"count": 11}})
	checkCount(1)
	change(&DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"r", JSON{"count": 12}}}})
	checkCount(2)
	commit(StorageWrite{Type: "document.retire", ID: id})
	checkCount(0)
	// The released fixture supplies a rewindable incarnation. Retirement
	// must not drop its earlier revisions or recreate a current value.
	commit(StorageWrite{Type: "document.retire", ID: 6})
	if err := writer.Close(bg); err != nil {
		t.Fatal(err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	if doc, found, err := view.DocumentAt(bg, id, CurrentDocumentPoint()); err != nil || found {
		t.Fatal(doc, found, err)
	}
	if err := view.Close(bg); err != nil {
		t.Fatal(err)
	}
	writer, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	h, err := Open(bg, writer, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(bg)
	if _, err := h.Root(bg, AgentChange{}); err != nil {
		t.Fatal(err)
	}
	if doc, found, err := writer.DocumentAt(bg, 6, DocumentPoint{Seq: 2}); err != nil || !found || doc.Version != 1 || !equalJSONValue(doc.Value, JSON{"text": "after"}) {
		t.Fatal(doc, found, err)
	}
}
