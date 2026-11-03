package durable

// Exact concrete types only: adapters must retain their Snapshot contract.
func directSQLiteTaskSnapshot(store Storage) bool {
	_, ok := store.(*SQLiteStorage)
	return ok
}
