package durable

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"
)

// c9f4c7b9 session-deferred-commits.test.ts:521-562. Preserve the complete
// a/b/c-immediate/d-fire/e-dependent-entry/f-close sequence and storage/publication
// replica assertions. The adapter now registers the actual private synchronous
// stored lane; the scenario/assertions and first-three receipt reads are intact.
// Those reads consume real callbacks, not synthetic publications or proof of
// synchrony (the separate observer controls verify publication-time delivery).
func TestMainSessionDeferredStoredObserverOracle(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := openMainDeadlineSession(t, b, nil)
		old, _ := mainDeferredLifecycleDefinitions(t)
		batches := mainDeferredLifecycleBatches(t, f.session)
		var mu sync.Mutex
		var published []CommitPublication
		arrived := make(chan struct{}, 16)
		if _, err := f.session.observeStoredCommits(sessionCommitObserver{
			advance: func(_ context.Context, publication CommitPublication) error {
				mu.Lock()
				published = append(published, publication)
				mu.Unlock()
				arrived <- struct{}{}
				return nil
			},
			end: func(WatchEnd) error { return nil },
		}); err != nil {
			t.Fatal(err)
		}
		appendText := func(text string, immediate bool) {
			t.Helper()
			_, err := f.session.commitPersistence(bg, func(tx *Tx) error {
				h, err := tx.AcquireDocument(old, 1, nil, nil)
				if err != nil {
					return err
				}
				return h.ApplyOperations([]Operation{{"a", []any{"text"}, text}})
			}, true, 0, !immediate)
			if err != nil {
				t.Fatal(err)
			}
		}
		appendText("a", false)
		appendText("b", false)
		appendText("c", true)
		watchReceive(t, arrived) // real recorded callback; scenario unchanged
		appendText("d", false)
		f.fire(t)
		watchReceive(t, arrived)
		appendText("e", false)
		if _, err := f.session.Commit(bg, func(tx *Tx) error {
			if _, err := tx.AcquireDocument(old, 1, nil, nil); err != nil {
				return err
			}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "test.note", Value: JSON{}})
		}); err != nil {
			t.Fatal(err)
		}
		watchReceive(t, arrived)
		appendText("f", false)
		if err := f.session.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		publications := append([]CommitPublication(nil), published...)
		mu.Unlock()
		stored := batches()
		if len(publications) != len(stored) {
			t.Errorf("stored observer count: %d publications for %d actual stored batches", len(publications), len(stored))
		}
		fromStorage, fromPublications := any(JSON{"text": ""}), any(JSON{"text": ""})
		for index, batch := range stored {
			found := false
			for _, write := range batch {
				if write.Document != nil && write.Document.ID == f.doc {
					fromStorage, found = write.Document.Value, true
				}
				if write.Delta != nil && write.Delta.ID == f.doc {
					var err error
					fromStorage, err = ApplyOperations(fromStorage, write.Delta.Ops, f.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					found = true
				}
			}
			if !found {
				t.Fatalf("stored batch %d lacks document change", index)
			}
			if index >= len(publications) {
				continue // missing publication already failed; still reconstruct real Storage
			}
			found = false
			for _, change := range publications[index].Documents {
				if change.Record.ID != f.doc {
					continue
				}
				found = true
				var err error
				fromPublications, err = ApplyOperations(fromPublications, change.Ops, f.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				if !equalJSONValue(fromPublications, fromStorage) || !equalJSONValue(change.Value, fromStorage) {
					t.Errorf("batch %d publication replica/value differs from Storage: %v %v %v", index, fromPublications, change.Value, fromStorage)
				}
			}
			if !found {
				t.Errorf("publication %d lacks document change", index)
			}
		}
		if !equalJSONValue(fromStorage, JSON{"text": "abcdef"}) {
			t.Error("stored final text", fromStorage)
		}
		var types [][]string
		for _, publication := range publications {
			var kinds []string
			for range publication.Documents {
				kinds = append(kinds, "document")
			}
			for _, write := range publication.Tables {
				if write.Entry != nil {
					kinds = append(kinds, "entry")
				} else {
					kinds = append(kinds, write.Op)
				}
			}
			sort.Strings(kinds)
			types = append(types, kinds)
		}
		want := [][]string{{"document"}, {"document"}, {"document", "entry"}, {"document"}}
		if !reflect.DeepEqual(types, want) {
			t.Error("stored publication change types", types, "want", want)
		}
	})
}
