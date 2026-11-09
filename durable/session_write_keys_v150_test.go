package durable

import (
	"path/filepath"
	"testing"
)

// Port of transaction.writeKeys() from pinned v1.5.0 src/session/transaction.ts,
// exercised through real typed-document commits so the key set follows the write
// shapes storage actually receives, rather than hand-built writes.

func writeKeysDefinition(t *testing.T) *DocumentDefinition {
	t.Helper()
	def, err := DefineDocument(DefinitionOptions{
		Kind:      "test.writekeys",
		Scope:     "conversation",
		Version:   1,
		Initial:   func(JSON) (JSON, error) { return JSON{"x": 0}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func TestSessionV150WriteKeys(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	store, err := openSQLiteTestDirectory(dir, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close(bg) })

	def := writeKeysDefinition(t)
	// A typed singleton conversation document: no key, so the address encodes the
	// key as null.
	address := DocumentAddress{Scope: "conversation", Owner: 1, Kind: "test.writekeys"}

	has := func(keys []string, want ...string) {
		t.Helper()
		for _, w := range want {
			found := false
			for _, k := range keys {
				if k == w {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("writeKeys missing %q in %v", w, keys)
			}
		}
	}
	lacks := func(keys []string, unwanted ...string) {
		t.Helper()
		for _, w := range unwanted {
			for _, k := range keys {
				if k == w {
					t.Errorf("writeKeys should not contain %q in %v", w, keys)
					break
				}
			}
		}
	}

	// Creating an incarnation keys the entry record (address and append), the
	// document address, its incarnation, and the scope it appeared in.
	var entryID, documentID ID
	var created []string
	if _, err := session.Commit(bg, func(tx *Tx) error {
		var e error
		if entryID, e = tx.MintID(); e != nil {
			return e
		}
		if err := tx.AppendEntry(Entry{ID: entryID, Conversation: 1, Kind: userEntryKind}); err != nil {
			return err
		}
		handle, err := tx.AcquireDocument(def, 1, nil, nil)
		if err != nil {
			return err
		}
		documentID = handle.id
		created, err = tx.writeKeys()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	has(created,
		entryKey(entryID), appendKey(1),
		documentKey(address), incarnationKey(documentID), "docscope:c1",
	)
	lacks(created, requestKey(1, ""), intentKey, rootsKey, allKeys)

	// Modifying an existing incarnation keys the address and the incarnation, but
	// not the scope: which incarnations a scope scan sees did not change.
	var modified []string
	if _, err := session.Commit(bg, func(tx *Tx) error {
		handle, err := tx.AcquireDocument(def, 1, nil, nil)
		if err != nil {
			return err
		}
		if err := handle.Set(JSON{"x": 1}); err != nil {
			return err
		}
		modified, err = tx.writeKeys()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	has(modified, documentKey(address), incarnationKey(documentID))
	lacks(modified, "docscope:c1", entryKey(entryID))

	// Retirement writes the scope again, because the incarnation left it.
	var retired []string
	if _, err := session.Commit(bg, func(tx *Tx) error {
		if err := tx.RetireDefinition(def, 1, nil); err != nil {
			return err
		}
		retired, err = tx.writeKeys()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	has(retired, documentKey(address), incarnationKey(documentID), "docscope:c1")
}
