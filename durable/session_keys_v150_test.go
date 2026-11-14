package durable

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// Consolidated from session_keys_v150_test.go.

// Port of the pinned v1.5.0 src/session/keys.ts key model. These assertions pin
// each key FORM against the reference, so a later install cannot silently
// change what a commit writes or a read touches. This is the key model only;
// Session does not gate reads on it yet.

func TestSessionV150KeyForms(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"task", taskKey(7), "task:7"},
		{"conversation", conversationKey(3), "conv:3"},
		{"entry", entryKey(11), "entry:11"},
		{"append", appendKey(3), "entry-append:3"},
		{"submission", submissionKey(5), "sub:5"},
		{"request", requestKey(3, "abc"), "req:3:abc"},
		{"requestEmpty", requestKey(3, ""), "req:3:"},
		{"incarnation", incarnationKey(9), "docid:9"},
		{"scopeSession", scopeKey("session", 0), "docscope:session"},
		{"scopeConversation", scopeKey("conversation", 4), "docscope:c4"},
		{"scopeTask", scopeKey("task", 6), "docscope:t6"},
		{"scopeUnknown", scopeKey("nope", 6), ""},
		{"ownedTask", ownedKey("6"), "owned:6"},
		{"ownedConversation", ownedKey(conversationNode(3)), "owned:c3"},
		{"roots", rootsKey, "roots"},
		{"intent", intentKey, "intent"},
		{"all", allKeys, "*"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: got %q want %q", c.name, c.got, c.want)
		}
	}
}

func TestSessionV150DocumentKeyIsAddressIdentity(t *testing.T) {
	// addressID mirrors JSON.stringify([kind, scope.kind, owner, key ?? null]).
	singleton := DocumentAddress{Scope: "conversation", Owner: 3, Kind: "pi.agent"}
	keyedEmpty := DocumentAddress{Scope: "conversation", Owner: 3, Kind: "pi.agent", Family: true}
	other := DocumentAddress{Scope: "task", Owner: 3, Kind: "pi.agent"}
	session := DocumentAddress{Scope: "session", Kind: "pi.usage"}

	if documentKey(singleton) == documentKey(keyedEmpty) {
		t.Error("singleton and keyed member with an empty key collide")
	}
	if documentKey(singleton) == documentKey(other) {
		t.Error("conversation and task addresses collide")
	}
	// A session scope has no owner upstream (owner encodes as null), so an owner
	// carried by mistake must not change the identity.
	if documentKey(session) != documentKey(DocumentAddress{Scope: "session", Owner: 1, Kind: "pi.usage"}) {
		t.Error("session scope should ignore a stray owner")
	}
	// Equality is exactly address equality, in both directions.
	same := DocumentAddress{Scope: "conversation", Owner: 3, Kind: "pi.agent"}
	if documentKey(singleton) != documentKey(same) {
		t.Error("equal addresses differ")
	}
	// The encoding is the reference's: a JSON array of the four components.
	want, err := json.Marshal([]any{"pi.agent", "conversation", 3, nil})
	if err != nil {
		t.Fatal(err)
	}
	if got := addressID(singleton); got != string(want) {
		t.Errorf("addressID: got %s want %s", got, want)
	}
}

func TestSessionV150TableKeys(t *testing.T) {
	writes := []Write{
		{Op: "create-conversation", Conversation: &Conversation{ID: 3}},
		{Op: "append-entry", Entry: &Entry{ID: 11, Conversation: 3}},
		{Op: "put-task", Task: &Task{ID: 7}},
		{Op: "put-submission", Submission: &Submission{ID: 5, Conversation: 3, RequestID: "r", RequestIDPresent: true}},
		// A present-but-empty request ID still names a key.
		{Op: "put-submission", Submission: &Submission{ID: 6, Conversation: 3, RequestIDPresent: true}},
		// An absent request ID names only the record.
		{Op: "put-submission", Submission: &Submission{ID: 8, Conversation: 3}},
		// Document writes are keyed by the transaction, which knows addresses.
		{Op: "put-document", Document: &Document{ID: 12}},
		{Op: "delta-document", Delta: &DocumentDelta{ID: 12}},
	}
	got := tableKeys(writes, nil)
	want := []string{
		"conv:3",
		"entry:11", "entry-append:3",
		"task:7",
		"sub:5", "req:3:r",
		"sub:6", "req:3:",
		"sub:8",
	}
	if len(got) != len(want) {
		t.Fatalf("keys=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys=%v want %v", got, want)
		}
	}
}

// Consolidated from session_write_keys_v150_test.go.

// Port of transaction.writeKeys() from pinned v1.5.0 src/session/transaction.ts,
// exercised through real typed-document commits so the key set follows the write
// shapes storage actually receives, rather than hand-built writes.

func writeKeysDefinition(t *testing.T) *DocumentDefinition {
	t.Helper()
	def, err := DefineDocument(DefinitionOptions{
		Kind:           "test.writekeys",
		Scope:          "conversation",
		Version:        1,
		Initial:        func(JSON) (JSON, error) { return JSON{"x": 0}, nil },
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
	if _, err := session.Commit(bg, func(tx *Tx) (any, error) {
		var e error
		if entryID, e = tx.MintID(); e != nil {
			return nil, e
		}
		if err := tx.AppendEntry(Entry{ID: entryID, Conversation: 1, Kind: userEntryKind}); err != nil {
			return nil, err
		}
		handle, err := tx.AcquireDocument(def, 1, nil, nil)
		if err != nil {
			return nil, err
		}
		documentID = handle.id
		created, err = tx.writeKeys()
		return nil, err
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
	if _, err := session.Commit(bg, func(tx *Tx) (any, error) {
		handle, err := tx.AcquireDocument(def, 1, nil, nil)
		if err != nil {
			return nil, err
		}
		if err := handle.Set(JSON{"x": 1}); err != nil {
			return nil, err
		}
		modified, err = tx.writeKeys()
		return nil, err
	}); err != nil {
		t.Fatal(err)
	}
	has(modified, documentKey(address), incarnationKey(documentID))
	lacks(modified, "docscope:c1", entryKey(entryID))

	// Retirement writes the scope again, because the incarnation left it.
	var retired []string
	if _, err := session.Commit(bg, func(tx *Tx) (any, error) {
		if err := tx.RetireDefinition(def, 1, nil); err != nil {
			return nil, err
		}
		retired, err = tx.writeKeys()
		return nil, err
	}); err != nil {
		t.Fatal(err)
	}
	has(retired, documentKey(address), incarnationKey(documentID), "docscope:c1")
}
