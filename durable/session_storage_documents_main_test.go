package durable

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type sourceDocumentStorage struct {
	*SQLiteStorage
	reads       []string
	omitContent bool
}

func (s *sourceDocumentStorage) FindDocument(ctx context.Context, address DocumentAddress, at DocumentPoint) (Document, bool, error) {
	s.reads = append(s.reads, fmt.Sprintf("find %s/%d/%s", address.Scope, address.Owner, address.Kind))
	return s.SQLiteStorage.FindDocument(ctx, address, at)
}

func (s *sourceDocumentStorage) DocumentAt(ctx context.Context, id ID, at DocumentPoint) (Document, bool, error) {
	s.reads = append(s.reads, fmt.Sprintf("load %d", id))
	if s.omitContent {
		return Document{}, false, nil
	}
	return s.SQLiteStorage.DocumentAt(ctx, id, at)
}

func TestSourcePublicStorageLoadsColdDefinitionByAddressAndID(t *testing.T) {
	bg := context.Background()
	sqliteStore(t, func(t *testing.T, f sqliteFixture) {
		store := &sourceDocumentStorage{SQLiteStorage: f.store.(*SQLiteStorage)}
		s, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close(bg) }()
		def, _ := mainDeferredLifecycleDefinitions(t)
		mainDeferredLifecycleAppend(t, s, def, 1, "held")
		if err := s.UnloadDocuments(bg); err != nil {
			t.Fatal(err)
		}
		address, err := definitionAddress(def, 1, nil)
		if err != nil {
			t.Fatal(err)
		}
		record, found, err := store.SQLiteStorage.FindDocument(bg, address, CurrentDocumentPoint())
		if err != nil || !found {
			t.Fatal(record, found, err)
		}
		stored, found, err := store.SQLiteStorage.DocumentAt(bg, record.ID, CurrentDocumentPoint())
		if err != nil || !found || !equalJSONValue(stored.Value, JSON{"text": "held"}) {
			t.Fatal(stored, found, err)
		}
		store.reads = nil
		cold, found, err := s.SnapshotDefinition(bg, def, 1, nil)
		if err != nil || !found || !equalJSONValue(cold.Copy(), JSON{"text": "held"}) {
			t.Fatal(cold, found, err)
		}
		warm, found, err := s.SnapshotDefinition(bg, def, 1, nil)
		if err != nil || !found || warm != cold {
			t.Fatal("warm snapshot lost readonly identity", warm, found, err)
		}
		watch, found, err := s.WatchDefinition(bg, def, 1, nil)
		if err != nil || !found {
			t.Fatal("warm watch baseline unavailable", found, err)
		}
		defer watch.Stop()
		watched, err := watch.Value()
		if err != nil || !equalJSONValue(watched.Copy(), JSON{"text": "held"}) {
			t.Fatal(watched, err)
		}
		want := []string{"find conversation/1/" + address.Kind, fmt.Sprintf("load %d", record.ID)}
		if !reflect.DeepEqual(store.reads, want) {
			t.Fatalf("public Storage document reads = %q, want %q", store.reads, want)
		}
		if err := s.UnloadDocuments(bg); err != nil {
			t.Fatal(err)
		}
		store.omitContent = true
		value, found, err := s.SnapshotDefinition(bg, def, 1, nil)
		wantError := fmt.Sprintf("Current document %d (%s) cannot be read", record.ID, record.Kind)
		if value != nil || found || err == nil || err.Error() != wantError {
			t.Fatalf("address resolved but content was missing: %v, %v, %v; want %s", value, found, err, wantError)
		}
	})
}
