package durable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Source-derived from storage-boundaries.json: the original raw Storage accepts
// an explicit, never-minted conversation ID and raises later minting above it.
// Root preinstallation is still a separate open D28 gap, so this packet does
// not assert freshRootPresent.
func TestV150StorageExplicitIDRaisesAllocator(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/storage-boundaries.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Conversation       Conversation
		ExplicitID         struct{ Stored int }
		StoredConversation struct{ ID ID }
		NextMint           ID
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		result, err := b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: fixture.Conversation}}}, nil)
		if err != nil || result.Refused != nil || result.Stored != fixture.ExplicitID.Stored {
			t.Fatalf("explicit ID: %+v %v", result, err)
		}
		if _, ok := snap(t, b.store).Conversations[fixture.StoredConversation.ID]; !ok {
			t.Fatal("explicit conversation not stored")
		}
		if got := mint(t, b.store); got != fixture.NextMint {
			t.Fatalf("next mint %d want %d", got, fixture.NextMint)
		}
		reopened := b.reopen()
		if _, ok := snap(t, reopened).Conversations[fixture.StoredConversation.ID]; !ok {
			t.Fatal("explicit conversation lost on reopen")
		}
		if got := mint(t, reopened); got != fixture.NextMint+1 {
			t.Fatalf("reopened mint %d", got)
		}
	})
}

func TestV150StorageExplicitIDGroupAndCollisions(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		// A later batch may reference an ID raised by an earlier batch; an
		// unminted ID below the high-water mark is also accepted when unused.
		result, err := b.store.Commit(bg, [][]StorageWrite{
			{{Type: "conversation", Value: Conversation{ID: 50}}},
			{{Type: "conversation", Value: Conversation{ID: 20}}},
		}, nil)
		if err != nil || result.Refused != nil || result.Stored != 2 {
			t.Fatalf("group: %+v %v", result, err)
		}
		// Global type collision refuses the batch and leaves the allocator.
		result, err = b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 80}}, {Type: "conversation", Value: Conversation{ID: 50}}}}, nil)
		if err != nil || result.Refused == nil || result.Stored != 0 {
			t.Fatalf("collision: %+v %v", result, err)
		}
		if _, ok := snap(t, b.store).Conversations[80]; ok {
			t.Fatal("refused batch partly stored")
		}
		if got := mint(t, b.store); got != 51 {
			t.Fatalf("refused explicit ID raised allocator: %d", got)
		}
		result, err = b.store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: ID(MaxID + 1)}}}}, nil)
		if err != nil || result.Refused == nil {
			t.Fatalf("out-of-domain ID: %+v %v", result, err)
		}
		if err := b.reopen().(*SQLiteStorage).Reclaim(bg); err != nil {
			t.Fatal(err)
		}
		if got := mint(t, b.reopen()); got != 52 {
			t.Fatalf("checkpoint mint %d", got)
		}
	})
}

func TestV150StorageExplicitIDRealRollback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	if err := store.exec("CREATE TRIGGER reject_explicit BEFORE INSERT ON frames WHEN NEW.kind=2 BEGIN SELECT RAISE(ABORT,'explicit rejected'); END;"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 99}}}}, nil); err == nil {
		t.Fatal("trigger did not fail")
	}
	if err := store.exec("DROP TRIGGER reject_explicit"); err != nil {
		t.Fatal(err)
	}
	if got := mint(t, store); got != 2 {
		t.Fatalf("rolled-back explicit ID raised allocator: %d", got)
	}
}

// Session ownership keeps native reservation: an unreserved ID inside a
// Session transaction is still rejected.
func TestV150SessionStillRequiresReservedIDs(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	if _, err = s.Commit(bg, func(tx *Tx) error { return tx.AppendEntry(Entry{ID: 99, Conversation: 1, Kind: "test", Value: JSON{}}) }); err == nil {
		t.Fatal("unreserved Session ID accepted")
	}
}

func TestV150StorageExplicitIDReplayRejectsForgedHighWater(t *testing.T) {
	for _, forged := range []int{98, 150} {
		path := filepath.Join(t.TempDir(), "store.sqlite")
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 99}}}}, nil); err != nil || result.Stored != 1 {
			t.Fatal(result, err)
		}
		if err := store.exec("UPDATE frames SET highwater=? WHERE kind=2", forged); err != nil {
			t.Fatal(err)
		}
		_ = store.Close(bg)
		if reopened, err := OpenSQLite(path, SQLiteOptions{}); err == nil {
			_ = reopened.Close(bg)
			t.Fatalf("forged high water %d accepted", forged)
		}
	}
}
