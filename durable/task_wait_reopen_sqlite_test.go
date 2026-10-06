//go:build cgo

package durable

import (
	"path/filepath"
	"testing"
)

func TestSQLiteWaitingTwoChildrenCloseReopenOrderedOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(bg) })
	checkWaitingChildrenReopen(t, backend{store: store}, func() Storage {
		reopened, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return reopened
	})
}
