package durable

import "testing"

// The general backends helper covers memory and journal. These integration
// boundaries also require an explicit SQLite run; it is not included there.
func utf16Backends(t *testing.T, fn func(*testing.T, backend)) {
	t.Helper()
	backends(t, fn)
	sqliteUTF16Backends(t, fn)
}
func utf16ReopenStore(t *testing.T, store Storage) Storage {
	t.Helper()
	if reopened, ok := sqliteUTF16ReopenStore(t, store); ok {
		return reopened
	}
	return reopenStoreAfterHarnessClose(t, store)
}
