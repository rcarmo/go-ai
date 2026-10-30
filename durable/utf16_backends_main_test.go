package durable

import (
	"path/filepath"
	"testing"
)

// The general backends helper covers memory and journal. These integration
// boundaries also require an explicit SQLite run; it is not included there.
func utf16Backends(t *testing.T, fn func(*testing.T, backend)) {
	t.Helper()
	backends(t, fn)
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "session.sqlite")
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close(bg) })
		fn(t, backend{store: store})
	})
}
func utf16ReopenStore(t *testing.T, store Storage) Storage {
	t.Helper()
	if native, ok := store.(*SQLiteStorage); ok {
		reopened, err := OpenSQLite(native.path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return reopened
	}
	return reopenStoreAfterHarnessClose(t, store)
}
