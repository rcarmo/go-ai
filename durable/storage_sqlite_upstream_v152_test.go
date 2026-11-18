package durable

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
