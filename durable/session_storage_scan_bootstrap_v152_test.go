package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

type sourceScanBootstrapStorage struct {
	Storage
	entryPages int
}

func (s *sourceScanBootstrapStorage) ScanEntries(ctx context.Context, q EntryQuery, n int, cursor Cursor) (Page[Entry], error) {
	s.entryPages++
	return s.Storage.ScanEntries(ctx, q, n, cursor)
}

func TestSourceV152PublicStorageOpensWithoutSnapshot(t *testing.T) {
	for _, name := range []string{"Documents", "Snapshot"} {
		if _, found := reflect.TypeFor[Storage]().MethodByName(name); found {
			t.Errorf("public Storage requires %s", name)
		}
	}
	sqliteStore(t, func(t *testing.T, f sqliteFixture) {
		var task TaskRecord
		if err := json.Unmarshal([]byte(`{"id":12,"conversationId":1,"kind":"test","version":1,"input":{},"background":false,"abortRequested":false,"state":{"status":"pending","checkpoint":{}}}`), &task); err != nil {
			t.Fatal(err)
		}
		first := []StorageWrite{{Type: "entry", Value: Entry{ID: 10, Conversation: 1, Kind: "note"}}, v152SessionDocument(11, "test.doc", nil, JSON{"text": "present"})}
		batches := [][]StorageWrite{first, {{Type: "task", Value: task}}}
		if result, err := f.store.Commit(bg, batches, nil); err != nil || result.Refused != nil || result.Stored != 2 {
			t.Fatal(result, err)
		}
		store := &sourceScanBootstrapStorage{Storage: f.store}
		s, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		if store.entryPages < 1 {
			t.Error("Session bootstrap bypassed public entry scans")
		}
		model, found, err := s.Task(bg, 12)
		if err != nil || !found || model.State.Status != "pending" {
			t.Fatal(model, found, err)
		}
		value, err := s.Commit(bg, func(tx *Tx) (any, error) {
			return tx.AppendEntry(1, EntryDraft{"kind": "new"})
		})
		if err != nil {
			t.Fatal(err)
		}
		entry, found, err := f.store.Entry(bg, value.(Entry).ID)
		if err != nil || !found || entry.CommitSeq != 4 || entry.Entry.Kind != "new" {
			t.Fatal(entry, found, err)
		}
	})
}

func TestSourceV152PublicStorageHistoricalReadUsesStoredIncarnation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, f sqliteFixture) {
		batches := [][]StorageWrite{
			{{Type: "entry", Value: Entry{ID: 10, Conversation: 1, Kind: "note"}}, {Type: "document.create", Record: &DocumentCreate{ID: 11, Kind: "test.history", Scope: DocumentCreateScope{Kind: "conversation", ConversationID: 1}, History: "rewindable", Fork: "asOf"}, Content: &DocumentContent{Kind: "base", Version: 1, Value: JSON{"text": "before"}}}},
			{{Type: "document.change", ID: 11, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"text"}, "after"}}}}},
		}
		if result, err := f.store.Commit(bg, batches, nil); err != nil || result.Refused != nil || result.Stored != 2 {
			t.Fatal(result, err)
		}
		store := &sourceScanBootstrapStorage{Storage: f.store}
		s, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		def, err := DefineDocument(DefinitionOptions{Kind: "test.history", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { t.Fatal("existing historical definition initialized"); return nil, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
		if err != nil {
			t.Fatal(err)
		}
		value, found, err := s.SnapshotDefinitionAsOf(bg, def, 1, nil, 10)
		if err != nil || !found || !equalJSONValue(value.Copy(), JSON{"text": "before"}) {
			t.Fatal(value, found, err)
		}
		current, found, err := s.SnapshotDefinition(bg, def, 1, nil)
		if err != nil || !found || !equalJSONValue(current.Copy(), JSON{"text": "after"}) {
			t.Fatal(current, found, err)
		}
	})
}
