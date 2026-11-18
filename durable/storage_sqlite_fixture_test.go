package durable

import (
	"os"
	"sync"
	"testing"
)

// openFrameSQLiteFixture constructs the historical namespace before using the
// real opener. Only tests that explicitly exercise private frames use it.
func openFrameSQLiteFixture(t testing.TB, path string, options SQLiteOptions) (*SQLiteStorage, error) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			return nil, err
		}
		writeSQLiteMigrationFixture(t, path, `CREATE TABLE "`+options.Prefix+`config" (id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL)`)
	}
	return OpenSQLite(path, options)
}

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
