package durable

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceV152SQLiteViewerRefreshKeepsWriterOwnership(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	writer, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	commit := func(writes ...StorageWrite) {
		t.Helper()
		result, err := writer.Commit(bg, [][]StorageWrite{writes}, nil)
		if err != nil || result.Stored != 1 || result.Refused != nil {
			t.Fatal(result, err)
		}
	}
	commit(StorageWrite{Type: "conversation", Value: Conversation{ID: 1}}, StorageWrite{Type: "entry", Value: Entry{ID: 10, Conversation: 1, Kind: "first"}})
	viewer, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("open viewer alongside active writer: %v", err)
	}
	defer viewer.Close(bg)
	if other, err := OpenSQLite(path, SQLiteOptions{}); err == nil {
		_ = other.Close(bg)
		t.Fatal("viewer displaced active writer lock")
	}
	query := EntryQuery{Conversation: 1}
	page, err := viewer.ScanEntries(bg, query, 10, nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].Kind != "first" {
		t.Fatal(page, err)
	}
	commit(StorageWrite{Type: "entry", Value: Entry{ID: 11, Conversation: 1, Kind: "second"}})
	page, err = viewer.ScanEntries(bg, query, 10, nil)
	if err != nil || len(page.Items) != 2 || page.Items[0].Kind != "second" {
		t.Fatal(page, err)
	}
	if err := writer.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
	commit(StorageWrite{Type: "entry", Value: Entry{ID: 14, Conversation: 1, Kind: "after-checkpoint"}})
	page, err = viewer.ScanEntries(bg, query, 10, nil)
	if err != nil || len(page.Items) != 3 || page.Items[0].Kind != "after-checkpoint" {
		t.Fatal(page, err)
	}
	if _, err := viewer.MintID(bg); !errors.Is(err, ErrReadOnlyStorage) {
		t.Fatal("viewer allocated an ID", err)
	}
	if _, err := viewer.Commit(bg, [][]StorageWrite{{{Type: "entry", Value: Entry{ID: 12, Conversation: 1, Kind: "forbidden"}}}}, nil); !errors.Is(err, ErrReadOnlyStorage) {
		t.Fatal("viewer attempted a write", err)
	}
	if err := viewer.Reclaim(bg); !errors.Is(err, ErrReadOnlyStorage) {
		t.Fatal("viewer attempted maintenance", err)
	}
	if err := viewer.Close(bg); err != nil {
		t.Fatal(err)
	}
	if other, err := OpenSQLite(path, SQLiteOptions{}); err == nil {
		_ = other.Close(bg)
		t.Fatal("closing viewer released writer's lock")
	}
	stored, found, err := writer.Entry(bg, 11)
	if err != nil || !found || stored.CommitSeq != 2 {
		t.Fatal(stored, found, err)
	}
	if _, found, err := writer.Entry(bg, 12); err != nil || found {
		t.Fatal(found, err)
	}
}

func TestSourceV152SQLiteViewerDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err == nil {
		_ = store.Close(bg)
		t.Fatal("viewer created a database")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
