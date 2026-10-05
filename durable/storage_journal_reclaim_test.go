package durable

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestJournalReclaimHistoryAllocatorAndLaterAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	store, err := OpenJournal(path, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	def, err := DefineDocument(DefinitionOptions{Kind: "reclaim.counter", Version: 1, Scope: "conversation", Fork: "asOf", History: "rewindable", Initial: func(JSON) (JSON, error) { return JSON{"count": 0}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	var id ID
	for step := 0; step < 25; step++ {
		_, err := session.Commit(bg, func(tx *Tx) error {
			handle, err := tx.AcquireDocument(def, 1, nil, nil)
			if err != nil {
				return err
			}
			id = handle.id
			return handle.Update(func(value JSON) error { value["count"] = step; return nil })
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Spent reservations with no logical records survive reclamation too.
	if _, err := session.MintID(bg); err != nil {
		t.Fatal(err)
	}
	before, err := store.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	ordinal, size := store.ordinal, store.size
	if err := store.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
	after, err := store.Snapshot(bg)
	if err != nil || !equalJSONValue(before, after) || store.ordinal != ordinal || store.size >= size {
		t.Fatal("reclaim changed state or did not shrink", err, size, store.size)
	}
	if err := store.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
	if err := session.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenJournal(path, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	snapshot, err := reopened.Snapshot(bg)
	if err != nil || !equalJSONValue(before, snapshot) {
		t.Fatal("checkpoint reopen", err)
	}
	history, ok, err := reopened.DocumentAt(bg, id, DocumentPoint{Seq: 1})
	if err != nil || !ok || !equalJSONValue(history.Value["count"], 0) {
		t.Fatal("historical document lost", history, err)
	}
	reserved, err := reopened.MintID(bg)
	if err != nil || uint64(reserved) != before.HighWater+1 {
		t.Fatal("spent ID reused", reserved, err)
	}
	seq, err := reopened.Apply(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &Entry{ID: reserved, Conversation: 1, Kind: "app.reclaimed", Value: JSON{}}}}})
	if err != nil || seq != before.Seq+1 {
		t.Fatal("post-checkpoint append", seq, err)
	}
	if err := reopened.Close(bg); err != nil {
		t.Fatal(err)
	}
	last, err := OpenJournal(path, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer last.Close(bg)
	snapshot, err = last.Snapshot(bg)
	if err != nil || snapshot.Seq != seq || snapshot.Entries[reserved].Kind != "app.reclaimed" {
		t.Fatal("later append replay", snapshot, err)
	}
}
func TestJournalReclaimCancelAndUncertainDirectorySync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	store, err := OpenJournal(path, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	spent, err := store.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if err := store.Reclaim(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	unchanged, err := os.ReadFile(store.path)
	if err != nil || string(before) != string(unchanged) {
		t.Fatal("cancel changed journal", err)
	}
	original := syncDirectory
	syncDirectory = func(string) error { return errors.New("sync failure") }
	err = store.Reclaim(bg)
	syncDirectory = original
	if !errors.Is(err, ErrPoisoned) {
		t.Fatal("uncertain reclaim acknowledged", err)
	}
	if _, err := store.MintID(bg); !errors.Is(err, ErrPoisoned) {
		t.Fatal("uncertain reclaim not sealed", err)
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	second, err := OpenJournal(path, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(bg)
	snapshot, err := second.Snapshot(bg)
	if err != nil || snapshot.HighWater != uint64(spent) {
		t.Fatal("adopted reclaim lost reservation", snapshot, err)
	}
}
func TestJournalReclaimedCheckpointRejectsEveryTruncationAndHeaderMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal")
	store, err := OpenJournal(path, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.MintID(bg); err != nil {
		t.Fatal(err)
	}
	if err = store.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(bg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(path, "journal.bin"))
	if err != nil {
		t.Fatal(err)
	}
	configEnd := int(binary.LittleEndian.Uint32(data[12:16])) + 72
	// A replacement is atomic: partial checkpoint images are corruption, never
	// harmless append tails that could discard spent IDs or logical history.
	for cut := configEnd; cut < len(data); cut++ {
		directory := filepath.Join(t.TempDir(), "cut")
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "journal.bin"), data[:cut], 0600); err != nil {
			t.Fatal(err)
		}
		opened, err := OpenJournal(directory, JournalOptions{})
		if opened != nil {
			opened.Close(bg)
		}
		if !errors.Is(err, ErrCorrupt) {
			t.Fatalf("checkpoint cut %d accepted: %v", cut, err)
		}
	}
	config := data[:configEnd]
	payload := data[configEnd+64 : len(data)-8]
	wrong := append(append([]byte{}, config...), frame(3, 3, 3, payload)...)
	directory := filepath.Join(t.TempDir(), "wrong")
	os.Mkdir(directory, 0700)
	os.WriteFile(filepath.Join(directory, "journal.bin"), wrong, 0600)
	opened, err := OpenJournal(directory, JournalOptions{})
	if opened != nil {
		opened.Close(bg)
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatal("checkpoint ordinal mismatch accepted", err)
	}
}
