package durable

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestSQLiteRootConfigVersionCompatibility pins the persisted-format policy that
// comes with removing the fabricated reserved root:
//
//   - a newly created store records config version 2 and holds no conversation,
//     so the reserved root only exists once its owner writes it;
//   - a store persisted before this split (version 1) never wrote a root, so the
//     implicit reserved root is restored for it and its existing data keeps the
//     semantics its writer intended;
//   - an unknown config version is corrupt rather than being guessed at.
func TestSQLiteRootConfigVersionCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var data []byte
	if err := store.db.QueryRowContext(bg, store.schemaSQL("SELECT value FROM config WHERE id=1")).Scan(&data); err != nil {
		t.Fatal(err)
	}
	var config sqliteConfig
	if err := decodeStrict(data, DefaultLimits(), 16<<10, &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != sqliteConfigVersion || sqliteConfigVersion != 2 {
		t.Fatalf("new store config version %d want 2", config.Version)
	}
	if _, ok := store.state.Conversations[1]; ok {
		t.Fatal("version 2 store fabricated the reserved root")
	}
	_, err = store.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: mint(t, store), Conversation: 1, Kind: "message", Value: JSON{}}}}})
	rejected(t, err)
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}

	// A version 1 file predates the split: it must still read, keep its implicit
	// root and accept new conversation-scoped data.
	if err := downgradeSQLiteConfigFile(t, path, 1); err != nil {
		t.Fatal(err)
	}
	legacy, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	state, err := legacy.Snapshot(bg)
	if err != nil || state.Seq != 0 || len(state.Conversations) != 1 {
		t.Fatalf("version 1 store %+v err=%v", state, err)
	}
	root, ok := state.Conversations[1]
	if !ok || root.ID != 1 {
		t.Fatalf("version 1 store lost its implicit root: %+v", state.Conversations)
	}
	_, err = legacy.Apply(bg, Batch{Writes: []Write{{Op: "create-conversation", Conversation: &Conversation{ID: 1}}}})
	rejected(t, err)
	id := mint(t, legacy)
	if _, err := legacy.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "message", Value: JSON{}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	if state, err := reopened.Snapshot(bg); err != nil || state.Seq != 1 || state.Entries[id].Conversation != 1 {
		t.Fatalf("version 1 data lost: %+v err=%v", state, err)
	}
}

// TestSQLiteUnknownConfigVersionIsCorrupt keeps the persisted-format gate closed:
// a config the native reader cannot interpret is never upgraded implicitly.
func TestSQLiteUnknownConfigVersionIsCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := downgradeSQLiteConfig(store, 3); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSQLite(path, SQLiteOptions{}); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("unknown config version: %v", err)
	}
}

// downgradeSQLiteConfigFile rewrites the config row of a closed store file so a
// reopen can be observed through the production read path.
func downgradeSQLiteConfigFile(t *testing.T, path string, version uint64) error {
	t.Helper()
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		return err
	}
	defer store.Close(bg)
	return downgradeSQLiteConfig(store, version)
}
