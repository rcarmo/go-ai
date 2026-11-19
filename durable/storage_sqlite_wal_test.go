package durable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The original sqlite-storage.test.ts keeps a reader open across writer close
// so the final-connection cleanup cannot mask a missing truncate checkpoint.
func TestSourceV152SQLiteAutocheckpointAndCloseTruncateWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wal.sqlite")
	pages := 1
	writer, err := OpenSQLite(path, SQLiteOptions{WALAutoCheckpointPages: &pages})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	var threshold int
	if err := writer.db.QueryRowContext(bg, "PRAGMA wal_autocheckpoint").Scan(&threshold); err != nil || threshold != 1 {
		t.Fatal(threshold, err)
	}
	seedRootConversation(t, writer)
	for i := 0; i < 20; i++ {
		entry := Entry{ID: mint(t, writer), Conversation: 1, Kind: "note", HasData: true, Data: JSON{"text": strings.Repeat("x", 32<<10), "index": i}}
		r, err := writer.Commit(bg, [][]StorageWrite{{{Type: "entry", Value: entry}}}, nil)
		if err != nil || r.Refused != nil || r.Stored != 1 {
			t.Fatal(r, r.Refused.AsError(), err)
		}
	}
	walPath := path + "-wal"
	info, err := os.Stat(walPath)
	if err != nil || info.Size() >= 512<<10 {
		t.Fatalf("autocheckpoint WAL: %v, %v", info, err)
	}
	viewer, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close(bg)
	var count int
	if err := viewer.db.QueryRowContext(bg, "SELECT count(*) FROM entries").Scan(&count); err != nil || count != 20 {
		t.Fatal(count, err)
	}
	if err := writer.Close(bg); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(walPath)
	if err != nil || info.Size() != 0 {
		t.Fatalf("writer close with an open viewer did not truncate WAL: %v, %v", info, err)
	}
}
