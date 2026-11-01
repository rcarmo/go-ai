//go:build cgo

package durable

import (
	"errors"
	"path/filepath"
	"testing"
)

func sqliteUTF16Backends(t *testing.T, fn func(*testing.T, backend)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session.sqlite")
		open := func() *SQLiteStorage {
			store, err := OpenSQLite(path, SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			return store
		}
		store := open()
		t.Cleanup(func() {
			if err := store.Close(bg); err != nil {
				// Harness ownership remains sealed after its Close; accept only joined closure.
				if errors.Is(err, ErrOwned) {
					select {
					case <-store.closeDone:
						return
					default:
					}
				}
				t.Error(err)
			}
		})
		fn(t, backend{store: store, reopen: func() Storage {
			if err := store.Close(bg); err != nil {
				t.Fatal(err)
			}
			store = open()
			return store
		}})
	})
}

func sqliteUTF16ReopenStore(t *testing.T, store Storage) (Storage, bool) {
	t.Helper()
	native, ok := store.(*SQLiteStorage)
	if !ok {
		return nil, false
	}
	reopened, err := OpenSQLite(native.path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return reopened, true
}
