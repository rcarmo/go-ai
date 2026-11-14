package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMainSessionReadImageGapFailsClosed(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 2)
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		if _, err := s.core.commitBatches(bg, batches, nil, true); err != nil {
			t.Fatal(err)
		}
		// A writer cannot skip the unpublished prefix merely by appending a
		// new legacy job. The global baseline gate now rejects before storage;
		// the existing adoption fence remains a second boundary.
		_, err = s.Commit(bg, func(tx *Tx) (any, error) {
			id, err := tx.MintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "gap", Value: JSON{}})
		})
		var failed *SessionFailed
		if err == nil || !errors.As(s.failure(), &failed) || failed.Cause != err {
			t.Fatal("image gap did not latch its own error", err, s.failure())
		}
		if err := s.readTasks(bg, func(Snapshot) error { t.Fatal("failed Session admitted a read"); return nil }); !errors.As(err, &failed) {
			t.Fatal("read lost failure fence", err)
		}
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		if end, err := s.Closed(ctx); err != nil || end.Reason != "failed" {
			t.Fatalf("failure did not close: %+v err=%v", end, err)
		}
		reopened := utf16ReopenStore(t, b.store)
		defer reopened.Close(bg)
		state, err := reopened.Snapshot(bg)
		if err != nil || state.Seq != 3 || len(state.Entries) != 2 {
			t.Fatal("gap appended stale legacy writes", state, err)
		}
	})
}

func TestMainSessionReadImageAdoptsOnlyReceiptedPrefix(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, ids := groupEntryBatches(t, b.store, 2)
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		frame, first, err := s.core.prepareCommit(s.readImage, batches[0], s.core.ordinal, true)
		if err != nil {
			t.Fatal(err)
		}
		_, second, err := s.core.prepareCommit(first, batches[1], frame.ordinal, true)
		if err != nil {
			t.Fatal(err)
		}
		if result, err := s.core.commitBatches(bg, batches, nil, true); err != nil || result.stored != 2 {
			t.Fatalf("group=%+v err=%v", result, err)
		}
		s.taskBookkeeping(func() {
			if err := s.adoptReadImage(second, 2); err == nil || s.readImage.Seq != 1 {
				t.Fatal("final raw image accepted for first receipt")
			}
			if err := s.adoptReadImage(second, 3); err == nil || s.readImage.Seq != 1 {
				t.Fatal("skipped publication accepted")
			}
			if err := s.adoptReadImage(first, 2); err != nil {
				t.Fatal(err)
			}
			if err := s.adoptReadImage(first, 2); err == nil {
				t.Fatal("duplicate publication accepted")
			}
		})
		if err := s.readTasks(bg, func(view Snapshot) error {
			if view.Seq != 2 || len(view.Entries) != 1 || view.Entries[ids[0]].Seq != 2 {
				t.Fatal("read did not stay at first publication", view.Seq)
			}
			if _, exists := view.Entries[ids[1]]; exists {
				t.Fatal("second unpublished entry visible")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		s.taskBookkeeping(func() {
			if err := s.adoptReadImage(second, 3); err != nil {
				t.Fatal(err)
			}
		})
		// Resume the real legacy path after the fixture publishes both parts.
		id, err := s.MintID(bg)
		if err != nil || id <= ids[1] {
			t.Fatal("allocator lost reservations", id, err)
		}
		value, err := s.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "after", Value: JSON{}})
		})
		if err != nil || value != nil {
			t.Fatal(value, err)
		}
		image, err := s.Snapshot(bg)
		seq := image.Seq
		if err != nil || seq != 4 {
			t.Fatalf("legacy commit seq=%d err=%v", seq, err)
		}
		if err := s.readTasks(bg, func(view Snapshot) error {
			if view.Seq != 4 || len(view.Entries) != 3 || view.HighWater < uint64(id) {
				t.Fatal("legacy publication or allocator not adopted")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
