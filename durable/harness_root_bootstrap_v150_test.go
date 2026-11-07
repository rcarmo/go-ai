package durable

import (
	"path/filepath"
	"testing"
)

// TestHarnessRootBootstrapsReservedConversation pins the caller side of the raw
// root split: Storage never fabricates conversation 1, so Harness.Root must
// materialize it through an ordinary committed create-conversation write, the way
// the original tx.createRootConversation() staged it inside the root commit.
func TestHarnessRootBootstrapsReservedConversation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := Open(bg, store, Options{})
	if err != nil {
		store.Close(bg)
		t.Fatal(err)
	}
	defer h.Close(bg)

	// Before the owner asks for the root, the store holds no conversation at all.
	if state, err := store.Snapshot(bg); err != nil || len(state.Conversations) != 0 {
		t.Fatalf("fresh store conversations %+v err=%v", state.Conversations, err)
	}
	if _, err := h.Conversation(bg, 1); err == nil {
		t.Fatal("root visible before its owner created it")
	}

	if _, err := h.Root(bg, AgentChange{}); err != nil {
		t.Fatal(err)
	}
	state, err := store.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	root, ok := state.Conversations[1]
	if !ok {
		t.Fatalf("Harness.Root did not store conversation 1: %+v", state.Conversations)
	}
	if root.ID != 1 {
		t.Fatalf("root record %+v", root)
	}
	// The bootstrap is a real commit: it consumes a sequence, exactly like the
	// original root transaction, and the reserved ID is never minted afterwards.
	if state.Seq == 0 {
		t.Fatal("root bootstrap was not a committed write")
	}


	// Reopening proves the root is persisted, not an in-memory convenience.
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	if _, ok := snap(t, reopened).Conversations[1]; !ok {
		t.Fatal("root bootstrap lost on reopen")
	}
	// The reserved identity is never handed out by the allocator.
	if id, err := reopened.MintID(bg); err != nil || id <= 1 {
		t.Fatalf("allocator reused the reserved root: %d err=%v", id, err)
	}
}
