package durable

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"testing"
)

// Source-shaped seam controls for f9a26365 session-observers.test.ts:77/87/105.
// These exercise the existing private local-commit publication lane, NOT a
// synchronous document observation. The latter still needs an address guard,
// pin/lifetime management, attachment semantics, error isolation and Harness
// wiring. No asynchronous DocumentWatch callback is used as a substitute.
func TestMainDocumentObserverLocalPublicationSeam(t *testing.T) {
	for _, mode := range []string{"absent", "stored", "memory"} {
		t.Run(mode, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			seedRootConversation(t, store)
			session, err := OpenSession(store)
			if err != nil {
				_ = store.Close(context.Background())
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close(context.Background()) })
			options := DefinitionOptions{
				Kind: "observe.p06.counter", Version: 1, Scope: "session",
				Initial:        func(JSON) (JSON, error) { return JSON{"count": 0}, nil },
				CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
			}
			owner := ID(0)
			if mode == "memory" {
				options.Scope, options.History, options.Fork = "conversation", "latest", "initial"
				owner = 1
			}
			definition, err := DefineDocument(options)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			set := func(count int, deferred bool) error {
				_, err := session.commitPersistence(ctx, func(tx *Tx) error {
					doc, err := tx.AcquireDocument(definition, owner, nil, nil)
					if err != nil {
						return err
					}
					return doc.ApplyOperations([]Operation{{"s", []any{"count"}, count}})
				}, true, 0, deferred, math.Inf(1))
				return err
			}
			if mode == "absent" {
				value, ok, err := session.SnapshotDefinition(ctx, definition, owner, nil)
				if err != nil || ok || value != nil {
					t.Fatalf("absent lookup: value=%v exists=%v err=%v", value, ok, err)
				}
				snapshot, err := session.Snapshot(ctx)
				if err != nil || len(snapshot.Documents) != 0 {
					t.Fatalf("absent lookup created a document: %v err=%v", snapshot.Documents, err)
				}
				return
			}
			initial := 1
			if mode == "memory" {
				initial = 0
			}
			if err := set(initial, false); err != nil {
				t.Fatal(err)
			}
			before, err := store.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var events []sessionObservedCommit
			detach, err := session.followSessionCommits(&sessionCommitListener{callback: func(_ context.Context, event sessionObservedCommit) error {
				events = append(events, event)
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer detach()
			want, deferred := 3, false
			if mode == "memory" {
				want, deferred = 5, true
			}
			if err := set(want, deferred); err != nil {
				t.Fatal(err)
			}
			// The private local listener is synchronous: by return it has seen
			// the adopted publication. Inspect every document change, not just
			// the first one, so unrelated work cannot be silently filtered out.
			if len(events) != 1 || len(events[0].documents) != 1 || len(events[0].tables) != 0 {
				t.Fatalf("local events: %+v", events)
			}
			change := events[0].documents[0]
			if change.Record.Retired || change.Value["count"] != json.Number(strconv.Itoa(want)) {
				t.Fatalf("count/retired: %+v", change)
			}
			wantOps := []Operation{{"s", []any{"count"}, json.Number(strconv.Itoa(want))}}
			if !reflect.DeepEqual(change.Ops, wantOps) {
				t.Fatalf("operations: got %#v want %#v", change.Ops, wantOps)
			}
			if deferred {
				if events[0].kind != "memory" || events[0].seq != nil {
					t.Fatalf("memory event: %+v", events[0])
				}
				after, err := store.Snapshot(ctx)
				if err != nil || after.Seq != before.Seq {
					t.Fatalf("held update reached storage: before=%d after=%d err=%v", before.Seq, after.Seq, err)
				}
				if err := session.UnloadDocuments(ctx); err != nil {
					t.Fatal(err)
				}
				if len(events) != 1 {
					t.Fatalf("storage flush duplicated the local event: %d", len(events))
				}
			} else if events[0].kind != "stored" || events[0].seq == nil || *events[0].seq <= before.Seq {
				t.Fatalf("stored event: %+v", events[0])
			}
		})
	}
}
