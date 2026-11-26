package durable

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
)

func requireV161CommitKind(t *testing.T, result CommitResult, want string) {
	t.Helper()
	if got := result.Kind(); got != want {
		t.Fatalf("commit kind = %q, want %q", got, want)
	}
}

// store-storage-commit.test.ts:129–156 and :184–203 use StoreStorage over
// SQLite. This native witness covers the public outcomes and receipt/entry
// order on native SQLite, not Chord's private outbox or statement layout.
func TestSourceV161SQLiteCommitStoredAndRefused(t *testing.T) {
	sqliteStore(t, func(t *testing.T, f sqliteFixture) {
		ctx := t.Context()
		mint := func() ID {
			t.Helper()
			id, err := f.store.MintID(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		entry := func(id ID, kind string) StorageWrite {
			return StorageWrite{Type: "entry", Value: Entry{ID: id, Conversation: 1, Kind: kind}}
		}
		var receipts []publicSQLiteReceipt
		capture := func(index int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{index, seq}) }
		before, err := f.store.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		firstThree := []ID{mint(), mint(), mint()}
		stored, err := f.store.Commit(ctx, [][]StorageWrite{{entry(firstThree[0], "m")}, {entry(firstThree[1], "m")}, {entry(firstThree[2], "m")}}, capture)
		if err != nil || stored.Stored != 3 || stored.Refused != nil || !slices.Equal(receipts, []publicSQLiteReceipt{{0, before.Seq + 1}, {1, before.Seq + 2}, {2, before.Seq + 3}}) {
			t.Fatal("wrong stored outcome or ordered receipts", stored, receipts, err)
		}
		for i, id := range firstThree {
			record, found, err := f.store.Entry(ctx, id)
			if err != nil || !found || record.Entry.Kind != "m" || record.CommitSeq != receipts[i].seq {
				t.Fatal("stored receipt does not reference its entry commit", id, record, found, err)
			}
		}
		prefix, insideRefusal, suffix := mint(), mint(), mint()
		receipts = nil
		// A valid command beside the duplicate must not leak from the refused
		// part; the third part should never be attempted.
		refused, err := f.store.Commit(ctx, [][]StorageWrite{
			{entry(prefix, "m")},
			{entry(insideRefusal, "not committed"), entry(prefix, "again")},
			{entry(suffix, "not attempted")},
		}, capture)
		if err != nil || refused.Stored != 1 || refused.Refused == nil || !slices.Equal(receipts, []publicSQLiteReceipt{{0, before.Seq + 4}}) {
			t.Fatal("wrong refused outcome or prefix receipts", refused, receipts, err)
		}
		var cause *StorageRejected
		if !errors.As(refused.Refused.AsError(), &cause) || cause.Reason != fmt.Sprintf("ID %d already belongs to entry", prefix) || refused.Refused.Value() != cause {
			t.Fatal("wrong native refusal cause", refused.Refused.Value())
		}
		checkPrefix := func(s nativeTestStorage) {
			t.Helper()
			state, err := s.Snapshot(ctx)
			if err != nil || state.Seq != before.Seq+4 {
				t.Fatal("refused part changed the SQLite sequence", state.Seq, err)
			}
			kept, found, err := s.Entry(ctx, prefix)
			if err != nil || !found || kept.Entry.Kind != "m" || kept.CommitSeq != before.Seq+4 {
				t.Fatal("wrong committed prefix after refusal", kept, found, err)
			}
			for _, id := range []ID{insideRefusal, suffix} {
				if leaked, found, err := s.Entry(ctx, id); err != nil || found {
					t.Fatal("refusal or suffix leaked a durable entry", leaked, found, err)
				}
			}
		}
		checkPrefix(f.store)
		reopened := f.reopen()
		checkPrefix(reopened)
		receipts = nil
		next, err := reopened.Commit(ctx, [][]StorageWrite{{entry(suffix, "m")}}, capture)
		if err != nil || next.Stored != 1 || next.Refused != nil || !slices.Equal(receipts, []publicSQLiteReceipt{{0, before.Seq + 5}}) {
			t.Fatal("store did not continue from the retained prefix", next, receipts, err)
		}
		requireV161CommitKind(t, stored, "stored")
		requireV161CommitKind(t, refused, "refused")
		requireV161CommitKind(t, next, "stored")
	})
}

// A false, null or undefined refusal still selects the refused branch. The
// classifier adds no fields or serialization changes to the Go result.
func TestV161CommitKindUsesRefusalPresence(t *testing.T) {
	for _, value := range []any{nil, false, 0, "", commitUndefinedValue{}} {
		refused := NewRefusal(value)
		result := CommitResult{1, refused}
		requireV161CommitKind(t, result, "refused")
		if result.Refused != refused || result.Refused.Value() != value {
			t.Fatal("original refusal identity or value changed", value, result.Refused.Value())
		}
	}
	result := CommitResult{0, nil}
	requireV161CommitKind(t, result, "stored")
	if raw, err := json.Marshal(result); err != nil || string(raw) != `{"Stored":0,"Refused":null}` {
		t.Fatal("Go commit result serialization changed", string(raw), err)
	}
}
