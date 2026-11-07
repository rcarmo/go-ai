package durable

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func groupEntryBatches(t *testing.T, store Storage, count int) ([]Batch, []ID) {
	t.Helper()
	// Fixtures address the reserved root, which its owner must create.
	seedRootConversation(t, store)
	batches, ids := make([]Batch, count), make([]ID, count)
	for index := range batches {
		id, err := store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		ids[index] = id
		batches[index] = Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "group", Value: JSON{"text": "original"}}}}}
	}
	return batches, ids
}

func TestInternalGroupReceiptsAndReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, ids := groupEntryBatches(t, b.store, 3)
		core := b.store.(nativeStorage).nativeCore()
		ordinal := core.ordinal
		var receipts [][2]uint64
		result, err := core.commitBatches(bg, batches, func(index int, seq uint64) error {
			receipts = append(receipts, [2]uint64{uint64(index), seq})
			return nil
		}, false)
		if err != nil || result.stored != 3 || result.lastSeq != 4 || result.refused != nil {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		if !reflect.DeepEqual(receipts, [][2]uint64{{0, 2}, {1, 3}, {2, 4}}) || core.ordinal != ordinal+3 {
			t.Fatalf("receipts=%v ordinal=%d", receipts, core.ordinal)
		}
		batches[0].Writes[0].Entry.Value["text"] = "mutated"
		reopened := b.reopen()
		state, err := reopened.Snapshot(bg)
		if err != nil || state.Seq != 4 || len(state.Entries) != 3 || state.Entries[ids[0]].Value["text"] != "original" {
			t.Fatalf("reopened=%+v err=%v", state, err)
		}
		id, err := reopened.MintID(bg)
		if err != nil || id <= ids[2] {
			t.Fatalf("reopened allocator=%v err=%v", id, err)
		}
	})
}

func TestInternalGroupPrefixRefusal(t *testing.T) {
	for _, refusedAt := range []int{0, 1, 2} {
		sqliteStore(t, func(t *testing.T, b sqliteFixture) {
			batches, ids := groupEntryBatches(t, b.store, 3)
			batches[refusedAt] = Batch{} // invalid input, not a backend failure
			core := b.store.(nativeStorage).nativeCore()
			var receipts []uint64
			result, err := core.commitBatches(bg, batches, func(index int, seq uint64) error {
				if index != len(receipts) {
					t.Error("receipt index out of order")
				}
				receipts = append(receipts, seq)
				return nil
			}, false)
			if err != nil || result.stored != refusedAt || result.refused == nil || result.refused.Unwrap() == nil || len(receipts) != refusedAt {
				t.Fatalf("prefix=%d result=%+v receipts=%v err=%v", refusedAt, result, receipts, err)
			}
			state, err := b.reopen().Snapshot(bg)
			if err != nil || state.Seq != uint64(refusedAt+1) || len(state.Entries) != refusedAt {
				t.Fatalf("reopened prefix=%+v err=%v", state, err)
			}
			for index, id := range ids {
				_, exists := state.Entries[id]
				if exists != (index < refusedAt) {
					t.Fatal("refused or later entry stored, or accepted entry missing")
				}
			}
		})
	}
}

func TestInternalGroupReceiptErrorPreservesDurability(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 3)
		core := b.store.(nativeStorage).nativeCore()
		failure := errors.New("receipt sink failed")
		var received []uint64
		_, err := core.commitBatches(bg, batches, func(index int, seq uint64) error {
			received = append(received, seq)
			if index == 1 {
				return failure
			}
			return nil
		}, false)
		if err != failure || !reflect.DeepEqual(received, []uint64{2, 3}) {
			t.Fatalf("received=%v err=%v", received, err)
		}
		// SQLite committed the complete transaction before the first receipt.
		// Memory/journal have confirmed only the two already delivered batches.
		want := 2
		if core.appendFrames != nil {
			want = 3
		}
		state, err := b.reopen().Snapshot(bg)
		if err != nil || len(state.Entries) != want || state.Seq != uint64(want+1) {
			t.Fatalf("reopened=%+v err=%v", state, err)
		}
	})
}

func TestInternalGroupRefusalPresence(t *testing.T) {
	for _, value := range []any{commitUndefinedValue{}, nil, false, 0, ""} {
		refusal := &commitRefusal{value: value}
		result := commitGroupResult{refused: refusal}
		err := result.refusalError()
		if result.refused == nil || err == nil || err.Error() == "" || !reflect.DeepEqual(result.refused.value, value) {
			t.Fatal("refusal value or presence lost")
		}
	}
	if (commitGroupResult{}).refusalError() != nil {
		t.Fatal("absent refusal projected as an error")
	}
	cause := errors.New("refused")
	if (commitGroupResult{refused: &commitRefusal{value: cause}}).refusalError() != cause || !errors.Is(&commitRefusal{value: cause}, cause) {
		t.Fatal("native cause lost")
	}
}

func TestInternalGroupOwnershipAndEmptyCall(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 1)
		core := b.store.(nativeStorage).nativeCore()
		result, err := core.commitBatches(bg, nil, nil, false)
		if err != nil || result.stored != 0 || result.refused != nil || result.lastSeq != 1 {
			t.Fatalf("empty call=%+v err=%v", result, err)
		}
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		if _, err := core.commitBatches(bg, batches, nil, false); !errors.Is(err, ErrOwned) {
			t.Fatal("group bypassed Session ownership", err)
		}
		if _, err := s.Commit(bg, func(tx *Tx) error { return tx.AppendEntry(*batches[0].Writes[0].Entry) }); err != nil {
			t.Fatal("owned legacy Apply path failed", err)
		}
	})
}

// Pinned store-storage-commit.test.ts:371: a backend failure rolls the entire
// SQLite call back; previously confirmed calls stand. An unknown outcome stops
// the store. A per-frame hook cannot inject a failure into appendFrames.
func TestInternalGroupAtomicBackendError(t *testing.T) {
	for _, knownRollback := range []bool{false, true} {
		sqliteStore(t, func(t *testing.T, b sqliteFixture) {
			seed, _ := groupEntryBatches(t, b.store, 1)
			core := b.store.(nativeStorage).nativeCore()
			if _, err := core.commitBatches(bg, seed, nil, false); err != nil {
				t.Fatal(err)
			}
			batches, _ := groupEntryBatches(t, b.store, 3)
			calls := 0
			failure := errors.New("unknown append outcome")
			if knownRollback {
				failure = reject("confirmed rejection")
			}
			core.appendFrames = func(frames []storageFrame) error {
				calls++
				if len(frames) != 3 {
					t.Errorf("frames=%d", len(frames))
				}
				return failure
			}
			received := 0
			result, err := core.commitBatches(bg, batches, func(int, uint64) error { received++; return nil }, false)
			if err == nil || result.refused != nil || received != 0 || core.state.Seq != 2 || calls != 1 || errors.Is(err, ErrPoisoned) == knownRollback {
				t.Fatalf("rollback=%v result=%+v receipts=%d calls=%d err=%v", knownRollback, result, received, calls, err)
			}
			if knownRollback && !errors.Is(err, failure) {
				t.Fatalf("rollback lost its cause: %v", err)
			}
			state, err := b.reopen().Snapshot(bg)
			if err != nil || state.Seq != 2 || len(state.Entries) != 1 {
				t.Fatalf("confirmed prior call lost: %+v error=%v", state, err)
			}
		})
	}
}

func TestInternalGroupBounds(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxFramePayloadBytes = 1024
	limits.MaxRecordBytes, limits.MaxDocumentBytes = 512, 512
	limits.MaxStringBytes, limits.MaxRequestIDBytes = 256, 128
	store, err := openTestSQLite(t, sqliteFixtureOptions{Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	batches, _ := groupEntryBatches(t, store, 32)
	for _, batch := range batches {
		batch.Writes[0].Entry.Value["pad"] = strings.Repeat("x", 200)
	}
	settledBytes := 0
	appendSeries := store.appendFrames
	store.appendFrames = func(frames []storageFrame) error {
		for _, frame := range frames {
			settledBytes += len(frame.payload)
		}
		return appendSeries(frames)
	}
	result, err := store.commitBatches(bg, batches, nil, false)
	if err != nil || result.refused == nil || result.stored <= 0 || result.stored >= len(batches) || settledBytes <= 0 || settledBytes > limits.MaxFramePayloadBytes {
		t.Fatalf("bounded prefix=%+v bytes=%d err=%v", result, settledBytes, err)
	}
	seq := store.state.Seq
	tooMany := make([]Batch, limits.MaxWrites+1)
	result, err = store.commitBatches(bg, tooMany, nil, false)
	if err != nil || result.refused == nil || result.stored != 0 || store.state.Seq != seq {
		t.Fatalf("batch-count limit=%+v err=%v", result, err)
	}
}

func TestInternalGroupAdmissionCancelAndClose(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	batches, _ := groupEntryBatches(t, store, 3)
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); _ = store.Close(bg) }()
	calls := 0
	appendSeries := store.appendFrames
	store.appendFrames = func(frames []storageFrame) error {
		calls++
		if calls == 1 {
			close(held)
			<-release
		}
		return appendSeries(frames)
	}
	first := make(chan error, 1)
	go func() {
		result, err := store.commitBatches(ctx, batches, nil, false)
		if err == nil && result.stored != 3 {
			err = errors.New("admitted group did not finish")
		}
		first <- err
	}()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("group did not reach settlement")
	}
	queuedCtx := &mainCommitAdmissionContext{Context: bg, entered: make(chan struct{})}
	queued := make(chan error, 1)
	go func() { _, err := store.commitBatches(queuedCtx, batches, nil, false); queued <- err }()
	<-queuedCtx.entered
	closing, stop := context.WithCancel(bg)
	stop()
	if err := store.Close(closing); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	cancel() // admission already happened; settlement must still finish
	unblock()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-queued; !errors.Is(err, ErrClosed) {
		t.Fatalf("queued group: %v", err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	if store.state.Seq != 4 || len(store.state.Entries) != 3 {
		t.Fatal("close lost admitted group")
	}
}
