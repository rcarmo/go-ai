//go:build cgo

package durable

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLite130PrefixIsolationReopenAndReclaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stores.db")
	for i, prefix := range []string{"", "one_", "two_"} {
		store, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
		if err != nil {
			t.Fatal(prefix, err)
		}
		initial, err := store.Snapshot(bg)
		if err != nil || initial.Seq != 0 {
			t.Fatal(prefix, initial.Seq, err)
		}
		s, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Commit(bg, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: id, Scope: "session", Kind: "app.prefix", Version: 1, Value: JSON{"index": i}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Reclaim(bg); err != nil {
			t.Fatal(err)
		}
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
	}
	for _, prefix := range []string{"two_", "", "one_"} {
		store, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix})
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.Snapshot(bg)
		if err != nil || snapshot.Seq != 1 || len(snapshot.Documents) != 1 {
			t.Fatal(prefix, snapshot, err)
		}
		if err = store.Close(bg); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLite130InvalidPrefixBeforeFilesystemEffects(t *testing.T) {
	for _, prefix := range []string{"1bad", "a-b", "a\";DROP TABLE config;--", "sqlite_test", "SQLITE_test", " white"} {
		path := filepath.Join(t.TempDir(), "new", "store.db")
		if store, err := OpenSQLite(path, SQLiteOptions{Prefix: prefix}); err == nil {
			store.Close(bg)
			t.Fatal("invalid prefix accepted", prefix)
		}
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Fatal("invalid prefix touched filesystem", prefix, err)
		}
	}
}
