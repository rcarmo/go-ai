package durable

import "sync"

// installSQLiteAcknowledgement injects transport acknowledgement behaviour
// after a real atomic SQLite append. It is a test seam, not another store.
func installSQLiteAcknowledgement(store *SQLiteStorage, ack func() error) {
	one, series := store.appendFrame, store.appendFrames
	store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if err := one(kind, ordinal, high, payload); err != nil {
			return err
		}
		return ack()
	}
	store.appendFrames = func(frames []storageFrame) error {
		if err := series(frames); err != nil {
			return err
		}
		return ack()
	}
}

// Match a lost terminal commit acknowledgement without failing an earlier ID
// reservation. SQLite batches may contain both kinds in one atomic append.
func installSQLiteCommitAcknowledgement(store *SQLiteStorage, ack func() error) {
	one, series := store.appendFrame, store.appendFrames
	store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if err := one(kind, ordinal, high, payload); err != nil {
			return err
		}
		if kind == 2 {
			return ack()
		}
		return nil
	}
	store.appendFrames = func(frames []storageFrame) error {
		if err := series(frames); err != nil {
			return err
		}
		for _, frame := range frames {
			if frame.kind == 2 {
				return ack()
			}
		}
		return nil
	}
}

func blockSQLiteAcknowledgement(store *SQLiteStorage, entered chan struct{}, release <-chan struct{}) {
	var once sync.Once
	installSQLiteAcknowledgement(store, func() error { once.Do(func() { close(entered); <-release }); return nil })
}

func sqliteFrameCount(store *SQLiteStorage) int64 {
	var count int64
	if err := store.db.QueryRowContext(bg, store.schemaSQL("SELECT count(*) FROM frames")).Scan(&count); err != nil {
		panic(err)
	}
	return count
}
