//go:build cgo

package durable

import (
	"path/filepath"
	"testing"
)

func TestSQLiteCancelledOwnerLateChildConversationWorkAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	checkCancelledOwnerLateWorkReopen(t, store, func() Storage {
		reopened, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return reopened
	})
}
