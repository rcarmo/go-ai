package durable

import (
	"reflect"
	"testing"
)

func TestMainSessionSerialReceiptCapturesExactCandidate(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, _ := groupEntryBatches(t, b.store, 2)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		var first, second commitReceipt
		var firstImage, secondImage Snapshot
		s.taskBookkeeping(func() {
			var err error
			first, firstImage, err = s.settleBatch(bg, batches[0], 0)
			if err != nil {
				t.Fatal(err)
			}
			second, secondImage, err = s.settleBatch(bg, batches[1], 1)
			if err != nil {
				t.Fatal(err)
			}
			if first.seq != 1 || second.seq != 2 || firstImage.Seq != 1 || len(firstImage.Entries) != 1 || secondImage.Seq != 2 || len(secondImage.Entries) != 2 {
				t.Fatal("serial receipt captured a later image")
			}
			if err := s.adoptReceipt(first, &secondImage); err == nil {
				t.Fatal("accepted future candidate")
			}
			if s.readImage.Seq != 0 {
				t.Fatal("failed adoption advanced publication")
			}
			if err := s.adoptReceipt(first, &firstImage); err != nil {
				t.Fatal(err)
			}
		})
		state, err := s.Snapshot(bg)
		if err != nil || state.Seq != 1 || len(state.Entries) != 1 {
			t.Fatal("raw future leaked", state, err)
		}
		s.taskBookkeeping(func() {
			if err := s.adoptReceipt(first, &firstImage); err == nil {
				t.Fatal("accepted duplicate receipt")
			}
			if err := s.adoptReceipt(second, &secondImage); err != nil {
				t.Fatal(err)
			}
			if reflect.ValueOf(s.readImage.Entries).Pointer() != reflect.ValueOf(secondImage.Entries).Pointer() {
				t.Fatal("trusted candidate was replayed or cloned")
			}
		})
		id, err := s.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		seq, err := s.Commit(bg, func(tx *Tx) error {
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "legacy", Value: JSON{}})
		})
		if err != nil || seq != 3 {
			t.Fatal(seq, err)
		}
		s.taskBookkeeping(func() {
			if s.readImage.Seq != 3 || reflect.ValueOf(s.readImage.Entries).Pointer() != reflect.ValueOf(s.core.state.Entries).Pointer() {
				t.Fatal("serial commit did not reuse validated native candidate")
			}
		})
	})
}
