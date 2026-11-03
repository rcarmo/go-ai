package tools

import (
	"github.com/rcarmo/go-ai/durable"
	"path/filepath"
	"testing"
)

func newSQLiteStore(t testing.TB) (*durable.SQLiteStorage, error) {
	t.Helper()
	return durable.OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"), durable.SQLiteOptions{})
}
