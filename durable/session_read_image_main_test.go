package durable

import "testing"

func TestMainContextBusyUsesPublishedImage(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		s := h.session
		id, err := s.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		batch := Batch{Writes: []Write{{Op: "put-task", Task: &Task{ID: id, Conversation: 1, Kind: "image.fixture", Status: "pending", Checkpoint: JSON{}}}}}
		if _, err := s.core.apply(bg, batch, true); err != nil {
			t.Fatal(err)
		}
		s.taskBookkeeping(func() {
			if s.contextBusy(1) {
				t.Fatal("unpublished raw task changed context idleness")
			}
		})
	})
}

// Source-derived boundary, not a translation of the complete async group test:
// the raw group settles on real storage, but no Session publication occurred.
func TestMainSessionInternalReadImageExcludesUnpublishedGroup(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		batches, _ := groupEntryBatches(t, b.store, 2)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		result, err := s.core.commitBatches(bg, batches, nil, true)
		if err != nil || result.stored != 2 || result.refused != nil {
			t.Fatalf("raw group=%+v err=%v", result, err)
		}
		raw, err := b.store.Snapshot(bg)
		if err != nil || raw.Seq != 2 || len(raw.Entries) != 2 {
			t.Fatal("raw group did not settle", raw.Seq, err)
		}
		if err := s.readTasks(bg, func(view Snapshot) error {
			if view.Seq != 0 || len(view.Entries) != 0 {
				t.Fatalf("unpublished raw data escaped into internal Session read: seq=%d entries=%d", view.Seq, len(view.Entries))
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}
