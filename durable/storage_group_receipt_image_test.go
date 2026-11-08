package durable

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestInternalGroupReceiptFramesPublishExactPrefixes(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		batches, ids := groupEntryBatches(t, b.store, 3)
		doc := mint(t, b.store)
		for index := range batches {
			batches[index].Writes = append(batches[index].Writes, Write{Op: "put-document", Document: &Document{ID: doc, Scope: "session", Kind: "receipt.prefix", Version: 1, Value: JSON{"n": index + 1}}})
		}
		reference, err := newTestSQLite(t)
		if err != nil {
			t.Fatal(err)
		}
		defer reference.Close(bg)
		referenceBatches, _ := groupEntryBatches(t, reference, 3)
		referenceDoc := mint(t, reference)
		if referenceDoc != doc {
			t.Fatal("reference allocator differs")
		}
		for index := range referenceBatches {
			referenceBatches[index].Writes = append(referenceBatches[index].Writes, Write{Op: "put-document", Document: &Document{ID: doc, Scope: "session", Kind: "receipt.prefix", Version: 1, Value: JSON{"n": index + 1}}})
		}
		seedRootConversation(t, b.store)
		s, err := OpenSession(b.store)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close(bg)
		var receipts []commitReceipt
		result, err := s.core.commitParts(bg, batches, func(r commitReceipt) error { receipts = append(receipts, r); return nil }, true)
		if err != nil || result.stored != 3 || len(receipts) != 3 {
			t.Fatal(result, err, len(receipts))
		}
		// The caller's containers must not be replay inputs, even after storage has
		// returned. Encoded receipt payloads are detached at strict preparation.
		batches[0].Writes[0].Entry.Value["text"] = "caller mutation"
		batches[0].Writes[1].Document.Value["n"] = "caller mutation"
		raw, err := b.store.Snapshot(bg)
		if err != nil || raw.Seq != 4 || len(raw.Entries) != 3 {
			t.Fatal(raw, err)
		}
		before, err := s.Snapshot(bg)
		if err != nil || before.Seq != 1 || len(before.Entries) != 0 {
			t.Fatal(before, err)
		}
		// Consume one receipt at a time. No final raw image or array of all prefix
		// candidates is used. Public reads must exclude every later durable part.
		for index, r := range receipts {
			if r.index != index || r.seq != uint64(index+2) {
				t.Fatal(r.index, r.seq)
			}
			previous := s.readImage
			if _, err := r.nextImage(previous, s.limits); err != nil {
				t.Fatal(err)
			}
			if previous.Seq != uint64(index+1) || len(previous.Entries) != index {
				t.Fatal("replay mutated base")
			}
			s.taskBookkeeping(func() {
				if err := s.adoptReceipt(r, nil); err != nil {
					t.Fatal(err)
				}
			})
			image, err := s.Snapshot(bg)
			if err != nil || image.Seq != r.seq || len(image.Entries) != index+1 || !equalJSONValue(image.Documents[doc].Value["n"], index+1) {
				t.Fatal(image, err)
			}
			for j, id := range ids {
				entry, ok := image.Entries[id]
				if ok != (j <= index) {
					t.Fatal("wrong published membership", id, ok, index)
				}
				if ok && (entry.Seq != uint64(j+2) || legacyEntryValue(entry)["text"] != "original") {
					t.Fatal("wrong detached prefix", entry)
				}
			}
			if _, err := reference.Apply(bg, referenceBatches[index]); err != nil {
				t.Fatal(err)
			}
			expected, err := reference.Snapshot(bg)
			if err != nil || image.Seq != expected.Seq || image.HighWater != expected.HighWater || !reflect.DeepEqual(image.Entries, expected.Entries) || !reflect.DeepEqual(image.Documents, expected.Documents) || !reflect.DeepEqual(image.DocumentRevisions, expected.DocumentRevisions) {
				t.Fatal("receipt prefix differs from sequential storage reference", err)
			}
			image.Documents[doc].Value["n"] = "public mutation"
			if !equalJSONValue(s.readImage.Documents[doc].Value["n"], index+1) {
				t.Fatal("public alias")
			}
		}
		id, err := s.MintID(bg)
		if err != nil || uint64(id) <= raw.HighWater {
			t.Fatal("allocator continuity", id, err)
		}
		seq, err := s.Commit(bg, func(tx *Tx) error {
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "after-prefix", Value: JSON{}})
		})
		if err != nil || seq != 5 {
			t.Fatal("legacy continuation", seq, err)
		}
	})
}

func TestInternalGroupReceiptFrameRefusalAndSinkFailure(t *testing.T) {
	for _, outcome := range []string{"refusal", "sink-error"} {
		t.Run(outcome, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				batches, _ := groupEntryBatches(t, b.store, 3)
				if outcome == "refusal" {
					batches[1].Writes = nil
				}
				core := b.store.(nativeStorage).nativeCore()
				base := core.state
				failure := errors.New("frame sink fixture")
				count := 0
				result, err := core.commitParts(bg, batches, func(r commitReceipt) error {
					next, e := r.nextImage(base, core.limits)
					if e != nil {
						return e
					}
					base = next
					count++
					if outcome == "sink-error" {
						return failure
					}
					return nil
				}, false)
				if count != 1 || base.Seq != 2 || len(base.Entries) != 1 {
					t.Fatal("wrong accepted publication", count, base.Seq)
				}
				wantStored := 1
				if outcome == "refusal" {
					if err != nil || result.refused == nil || result.stored != 1 {
						t.Fatal(result, err)
					}
				} else {
					if !errors.Is(err, failure) {
						t.Fatal(err)
					}
					if core.appendFrames != nil {
						wantStored = 3
					}
				}
				state, e := b.store.Snapshot(bg)
				if e != nil || len(state.Entries) != wantStored {
					t.Fatal("wrong durable prefix", len(state.Entries), e)
				}
			})
		})
	}
}

func TestInternalReceiptImageRejectsWrongFrame(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	batches, _ := groupEntryBatches(t, store, 1)
	base := store.state
	var receipt commitReceipt
	if _, err := store.commitParts(bg, batches, func(r commitReceipt) error { receipt = r; return nil }, false); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"index", "sequence", "payload-sequence", "kind", "ordinal", "high", "payload", "gap"} {
		t.Run(kind, func(t *testing.T) {
			r, b := receipt, base
			switch kind {
			case "index":
				r.index = -1
			case "sequence":
				r.seq++
			case "payload-sequence":
				// The frame payload must disagree with the receipt's own sequence.
				r.frame.payload = []byte(fmt.Sprintf(`{"seq":%d,"writes":[]}`, r.seq+1))
			case "kind":
				r.frame.kind = 1
			case "ordinal":
				r.frame.ordinal = 0
			case "high":
				r.frame.high = MaxID + 1
			case "payload":
				// Empty arrays are valid commits; a null write list is corrupt.
				r.frame.payload = []byte(`{"seq":1,"writes":null}`)
			case "gap":
				// A base at the receipt's own sequence leaves no contiguous slot.
				b.Seq = r.seq
			}
			if _, err := r.nextImage(b, store.limits); err == nil {
				t.Fatal("invalid receipt accepted")
			}
			if base.Seq != 1 || len(base.Entries) != 0 {
				t.Fatal("failed reconstruction changed base")
			}
		})
	}
}

func TestInternalReceiptImageAcceptsEmptyPart(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	base := store.state
	seen := false
	result, err := store.commitParts(bg, []Batch{{Writes: []Write{}}}, func(receipt commitReceipt) error {
		next, err := receipt.nextImage(base, store.limits)
		if err != nil {
			return err
		}
		if next.Seq != 1 || next.HighWater != base.HighWater || len(next.Conversations) != len(base.Conversations) || len(next.Entries) != 0 {
			t.Fatal("wrong empty prefix", next)
		}
		seen = true
		return nil
	}, false)
	if err != nil || result.refused != nil || result.stored != 1 || !seen {
		t.Fatalf("empty receipt: %+v %v seen=%v", result, err, seen)
	}
}
