package durable

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

// Source-derived publish() stored-vs-observed split: a dependent entry stores
// old holds, but local listeners see only this transaction's own changes.
func TestMainSessionLocalMixedOwnChanges(t *testing.T) {
	for _, own := range []string{"", "b"} {
		name := "read-dependent"
		if own != "" {
			name = "changed-dependent"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				f := openMainDeadlineSession(t, b, nil)
				def, _ := mainDeferredLifecycleDefinitions(t)
				var mu sync.Mutex
				var local []sessionObservedCommit
				var stored []sessionPublication
				detach, err := f.session.followSessionCommits(&sessionCommitListener{callback: func(_ context.Context, e sessionObservedCommit) error {
					mu.Lock()
					local = append(local, e)
					mu.Unlock()
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				defer detach()
				arrived := make(chan struct{}, 4)
				if _, err = f.session.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p sessionPublication) error {
					mu.Lock()
					stored = append(stored, p)
					mu.Unlock()
					arrived <- struct{}{}
					return nil
				}, end: func(WatchEnd) error { return nil }}); err != nil {
					t.Fatal(err)
				}
				if err = f.append(f.doc, "a"); err != nil {
					t.Fatal(err)
				}
				if _, err = f.session.Commit(bg, func(tx *Tx) error {
					handle, err := tx.AcquireDocument(def, 1, nil, nil)
					if err != nil {
						return err
					}
					if own != "" {
						if err = handle.ApplyOperations([]Operation{{"a", []any{"text"}, own}}); err != nil {
							return err
						}
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
				mu.Lock()
				defer mu.Unlock()
				if len(local) != 2 || len(stored) != 1 {
					t.Error("memory then own-stored / host only stored", len(local), len(stored))
					return
				}
				event, host := local[1], stored[0]
				if event.kind != "stored" || event.seq == nil || *event.seq != host.Seq || len(event.tables) != 1 || event.tables[0].Entry == nil || len(host.Tables) != 1 {
					t.Error("own stored table shape/sequence", event, host)
				}
				wantOwn := 0
				if own != "" {
					wantOwn = 1
				}
				if len(event.documents) != wantOwn {
					t.Error("dependent store re-showed old document ops", event.documents)
				} else if own != "" && !reflect.DeepEqual(event.documents[0].Ops, []Operation{{"a", []any{"text"}, own}}) {
					t.Error("own operations", event.documents)
				}
				if len(host.Documents) != 1 || !reflect.DeepEqual(host.Documents[0].Ops, []Operation{{"a", []any{"text"}, "a" + own}}) {
					t.Error("host must receive all stored operations", host.Documents)
				}
			})
		})
	}
}
