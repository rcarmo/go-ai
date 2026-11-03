package durable

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

// 3e0eec29 (unchanged from c9), session-deferred-commits.test.ts:564-596.
// Entire a/b/c/d progression, watch values, kinds and literal OWN operations.
// Native reserved conversation/setup and real asynchronous watch receipts adapt
// source open()/flush(); the local lane itself must be synchronous.
func TestMainSessionDeferredLocalObserverOracle(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f := openMainDeadlineSession(t, b, nil)
		def, _ := mainDeferredLifecycleDefinitions(t)
		watch, ok, err := f.session.WatchDefinition(bg, def, 1, nil)
		if err != nil || !ok {
			t.Fatal(ok, err)
		}
		defer watch.Stop()
		var mu sync.Mutex
		var values []string
		watched := make(chan struct{}, 16)
		if err := watch.Start(func(_ context.Context, value JSON, _ []Operation) error {
			mu.Lock()
			values = append(values, value["text"].(string))
			mu.Unlock()
			watched <- struct{}{}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		var observed []sessionObservedCommit
		arrived := make(chan struct{}, 16)
		detach, err := f.session.followSessionCommits(&sessionCommitListener{callback: func(_ context.Context, event sessionObservedCommit) error {
			mu.Lock()
			observed = append(observed, event)
			mu.Unlock()
			arrived <- struct{}{}
			return nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		defer detach()
		from := len(observed) // Source discards setup publications.
		for _, text := range []string{"a", "b", "c", "d"} {
			_, err := f.session.commitPersistence(bg, func(tx *Tx) error {
				h, err := tx.AcquireDocument(def, 1, nil, nil)
				if err != nil {
					return err
				}
				return h.ApplyOperations([]Operation{{"a", []any{"text"}, text}})
			}, true, 0, text != "d")
			if err != nil {
				t.Fatal(err)
			}
			watchReceive(t, watched) // Source flush: actual native watch callback.
		}
		// Source's final flush also drains the old API adapter's real stored
		// callback. No missing memory publication is invented by this receipt.
		watchReceive(t, arrived)
		f.fire(t)
		mu.Lock()
		defer mu.Unlock()
		if !reflect.DeepEqual(values, []string{"a", "ab", "abc", "abcd"}) {
			t.Error("watch values", values)
		}
		seen := observed[from:]
		var kinds []string
		var operations [][][]Operation
		for _, event := range seen {
			kinds = append(kinds, event.kind)
			// Source maps EVERY change: a non-document/non-change produces
			// false and fails its literal expectation, never gets filtered out.
			if len(event.tables) != 0 {
				t.Error("unexpected non-document change", event.tables)
			}
			var documents [][]Operation
			for _, change := range event.documents {
				if change.Value == nil {
					t.Error("unexpected retired change", change)
				}
				documents = append(documents, change.Ops)
			}
			operations = append(operations, documents)
		}
		if !reflect.DeepEqual(kinds, []string{"memory", "memory", "memory", "stored"}) {
			t.Error("local kinds", kinds)
		}
		want := [][][]Operation{
			{{{"a", []any{"text"}, "a"}}},
			{{{"a", []any{"text"}, "b"}}},
			{{{"a", []any{"text"}, "c"}}},
			{{{"a", []any{"text"}, "d"}}},
		}
		if !reflect.DeepEqual(operations, want) {
			t.Error("local OWN operations", operations, "want", want)
		}
		// Native union representation: memory has no seq/tables. This is an
		// additional shape control, separate from the literal source assertions.
		for _, event := range seen {
			if event.kind == "memory" && (event.seq != nil || len(event.tables) != 0) {
				t.Error("memory leaked durable fields", event)
			}
			if event.kind == "stored" && (event.seq == nil || *event.seq == 0) {
				t.Error("stored sequence missing", event)
			}
		}
	})
}
