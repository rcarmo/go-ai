//go:build cgo

package durable

import "testing"

func TestSQLite120DirectRequestMatrix(t *testing.T) {
	store, e := OpenSQLite(t.TempDir()+"/store.db", SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close(bg)
	storageRequestMatrix120(t, store)
}
