package durable

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type sqliteCopyReadFault struct {
	sqliteConnection
	err       error
	entry     ID
	sawPrefix bool
}

func (f *sqliteCopyReadFault) QueryContext(ctx context.Context, text string, args ...any) (*sql.Rows, error) {
	if strings.Contains(text, `SELECT record FROM "documents" WHERE id = ?`) {
		var count int
		if err := f.sqliteConnection.QueryRowContext(ctx, `SELECT count(*) FROM entries WHERE id=?`, f.entry).Scan(&count); err != nil {
			return nil, err
		}
		f.sawPrefix = count == 1
		return nil, f.err
	}
	return f.sqliteConnection.QueryContext(ctx, text, args...)
}

// sqlite-storage.test.ts: a failing copy-source read rolls back even the
// earlier batch flushed to make its writes available to the copy.
func TestSourceV152SQLiteCopyReadRollsBackFlushedPrefix(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	seedRootConversation(t, s)
	record := func(id, conv ID) *DocumentCreate {
		return &DocumentCreate{ID: id, Kind: "copied", Scope: DocumentCreateScope{Kind: "conversation", ConversationID: conv}, History: "rewindable", Fork: "asOf"}
	}
	sourceID := mint(t, s)
	r, err := s.Commit(bg, [][]StorageWrite{{{Type: "document.create", Record: record(sourceID, 1), Content: &DocumentContent{Kind: "base", Version: 1, Value: JSON{"n": 1}}}}}, nil)
	if err != nil || r.Refused != nil || r.Stored != 1 {
		t.Fatal(r, err)
	}
	entry, child, target := mint(t, s), mint(t, s), mint(t, s)
	batches := [][]StorageWrite{{{Type: "entry", Value: Entry{ID: entry, Conversation: 1, Kind: "note"}}}, {{Type: "conversation", Value: Conversation{ID: child}}, {Type: "document.copy", Record: record(target, child), Source: &DocumentCopySource{ID: sourceID, At: CurrentDocumentPoint()}}}}
	raw := s.db
	fault := &sqliteCopyReadFault{sqliteConnection: raw, err: errors.New("copy source read fault"), entry: entry}
	s.db = fault
	var receipts []publicSQLiteReceipt
	receive := func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) }
	r, err = s.Commit(bg, batches, receive)
	if want := "Document copy " + idText(target) + " was rejected"; err == nil || err.Error() != want || errors.Unwrap(err) != fault.err {
		t.Fatalf("copy fault=%v, want %q wrapping the exact query error", err, want)
	}
	if !fault.sawPrefix || r.Refused != nil || r.Stored != 0 || len(receipts) != 0 {
		t.Fatalf("saw flushed prefix=%v, result=%+v, receipts=%v", fault.sawPrefix, r, receipts)
	}
	s.db = raw
	if _, ok, err := s.Entry(bg, entry); err != nil || ok {
		t.Fatal(ok, err)
	}
	if _, ok, err := s.Conversation(bg, child); err != nil || ok {
		t.Fatal(ok, err)
	}
	if _, ok, err := s.DocumentAt(bg, target, CurrentDocumentPoint()); err != nil || ok {
		t.Fatal(ok, err)
	}
	r, err = s.Commit(bg, batches, receive)
	if err != nil || r.Refused != nil || r.Stored != 2 || !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 3}, {1, 4}}) {
		t.Fatal(r, receipts, err)
	}
	if value, ok, err := s.DocumentAt(bg, target, CurrentDocumentPoint()); err != nil || !ok || value.Version != 1 || !equalJSONValue(value.Value, JSON{"n": 1}) {
		t.Fatal(value, ok, err)
	}
}

// A later copy observes a prior part within the same transaction, while an
// as-of copy still picks the specified stored revision.
func TestSourceV152SQLiteCopiesFlushedDeltaAtExactPoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "copy.sqlite")
	s, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	seedRootConversation(t, s)
	record := func(id, conv ID) *DocumentCreate {
		return &DocumentCreate{ID: id, Kind: "copied", Scope: DocumentCreateScope{Kind: "conversation", ConversationID: conv}, History: "rewindable", Fork: "asOf"}
	}
	source := mint(t, s)
	commit := func(b [][]StorageWrite) {
		t.Helper()
		r, err := s.Commit(bg, b, nil)
		if err != nil || r.Refused != nil || r.Stored != len(b) {
			t.Fatal(r, r.Refused.AsError(), err)
		}
	}
	commit([][]StorageWrite{{{Type: "document.create", Record: record(source, 1), Content: &DocumentContent{Kind: "base", Version: 1, Value: JSON{"n": 1}}}}})
	currentOwner, current, historicalOwner, historical := mint(t, s), mint(t, s), mint(t, s), mint(t, s)
	copyPart := func(id, owner ID, at DocumentPoint) []StorageWrite {
		return []StorageWrite{{Type: "conversation", Value: Conversation{ID: owner}}, {Type: "document.copy", Record: record(id, owner), Source: &DocumentCopySource{ID: source, At: at}}}
	}
	commit([][]StorageWrite{{{Type: "document.change", ID: source, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"n"}, 2}}}}}, copyPart(current, currentOwner, CurrentDocumentPoint()), copyPart(historical, historicalOwner, DocumentPoint{Seq: 2})})
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	viewer, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close(bg)
	for _, want := range []struct {
		id     ID
		number int
		seq    uint64
	}{{current, 2, 4}, {historical, 1, 5}} {
		got, ok, err := viewer.DocumentAt(bg, want.id, CurrentDocumentPoint())
		if err != nil || !ok || got.CreatedAt != want.seq || got.Version != 1 || got.DeltasSinceBase != 0 || !equalJSONValue(got.Value, JSON{"n": want.number}) {
			t.Fatal(got, ok, err, want)
		}
	}
}
