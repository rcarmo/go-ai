package durable

import "testing"

func utf16ReopenStore(t *testing.T, store Storage) Storage {
	t.Helper()
	return reopenStoreAfterHarnessClose(t, store)
}
