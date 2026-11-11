package durable

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// Pinned session-watches.test.ts:369-414, 678-720: retirement retains the
// preceding value, follows the address, and never applies old-shape operations
// to a new incarnation. Native callbacks remain detached rather than JS aliases.
func TestMainWatchAddressRetirementRecreation(t *testing.T) {
	for _, sameCommit := range []bool{false, true} {
		for _, newVersion := range []bool{false, true} {
			name := "separate"
			if sameCommit {
				name = "same-commit"
			}
			if newVersion {
				name += "-new-version"
			}
			t.Run(name, func(t *testing.T) {
				sqliteStore(t, func(t *testing.T, b sqliteFixture) {
					seedRootConversation(t, b.store)
					s, err := OpenSession(b.store)
					if err != nil {
						t.Fatal(err)
					}
					defer s.Close(bg)
					def := watchDoc(t, s)
					w := watchAcquire(t, s, def)
					defer w.Stop()
					oldID := w.id
					target := def
					if newVersion {
						target = checkpointDef(t, DefinitionOptions{Kind: "watch.test", Scope: "session", Version: 2,
							Initial:        func(JSON) (JSON, error) { return JSON{"n": 0, "new": true}, nil },
							Migrate:        func(v JSON, _ uint64) (JSON, error) { v["new"] = true; return v, nil },
							CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
						})
					}
					retire := func(tx *Tx) error {
						h, e := tx.AcquireDocument(def, 0, nil, nil)
						if e != nil {
							return e
						}
						if e = h.ApplyOperations([]Operation{{"s", []any{"n"}, 99}}); e != nil {
							return e
						}
						return tx.RetireDefinition(target, 0, nil)
					}
					recreate := func(tx *Tx) error {
						h, e := tx.AcquireDocument(target, 0, nil, nil)
						if e != nil {
							return e
						}
						return h.ApplyOperations([]Operation{{"s", []any{"n"}, 10}})
					}
					// Delay start: the baseline must stay at acquisition even after publication.
					if sameCommit {
						checkpointCommit(t, s, func(tx *Tx) error {
							if e := retire(tx); e != nil {
								return e
							}
							return recreate(tx)
						})
					} else {
						checkpointCommit(t, s, retire)
					}
					v, e := detachedDocumentWatchValue(w)
					if e != nil || v["n"].(json.Number) != "0" {
						t.Fatal(v, e)
					}
					if s.documentPinned(oldID) {
						t.Fatal("retired incarnation still pinned")
					}
					w.mu.Lock()
					queued := w.pending // retain the backing array to check consumed slots
					w.mu.Unlock()
					frames := make(chan rawWatchFrame, 8)
					if e = startDetachedDocumentFrames(w, func(_ context.Context, f rawWatchFrame) error { frames <- f; return nil }); e != nil {
						t.Fatal(e)
					}
					f := watchReceive(t, frames)
					w.mu.Lock()
					for _, consumed := range queued {
						if consumed.Value != nil || consumed.ownedValue != nil {
							t.Error("consumed queue slot retains an incarnation")
						}
					}
					w.mu.Unlock()
					if f.Kind != "value" || !f.Retired {
						t.Fatal("missing retirement tag", f)
					}
					if !sameCommit {
						if f.Value["n"].(json.Number) != "0" || len(f.Ops) != 0 {
							t.Fatal("published retiring mutation", f)
						}
						if _, ended := w.End(); ended {
							t.Fatal("retirement ended watch")
						}
						checkpointCommit(t, s, recreate)
						f = watchReceive(t, frames)
						if f.Retired {
							t.Fatal("retirement repeated across commits", f)
						}
					}
					if f.Value["n"].(json.Number) != "10" || len(f.Ops) != 1 || f.Ops[0][0] != "r" {
						t.Fatal("missing recreated root", f)
					}
					if newVersion && f.Value["new"] != true {
						t.Fatal("wrong shape", f)
					}
					w.mu.Lock()
					newID := w.id
					w.mu.Unlock()
					if newID == 0 || newID == oldID || !s.documentPinned(newID) {
						t.Fatal("incarnation pin not transferred")
					}
					// Callback data, watch Value and committed values must not alias.
					f.Value["n"] = -1
					watchSet(t, s, target, 11)
					f = watchReceive(t, frames)
					if f.Retired || f.Value["n"].(json.Number) != "11" || len(f.Ops) != 1 || f.Ops[0][0] != "s" {
						t.Fatal("lost incremental follow", f)
					}
					v, e = detachedDocumentWatchValue(w)
					if e != nil || v["n"].(json.Number) != "11" {
						t.Fatal(v, e)
					}
					f.Value["n"] = -2
					v, e = detachedDocumentWatchValue(w)
					if e != nil || v["n"].(json.Number) != "11" {
						t.Fatal("callback mutated watch", v, e)
					}
					w.Stop()
					if s.documentPinned(newID) {
						t.Fatal("stopped watch retained pin")
					}
				})
			})
		}
	}
}

func TestMainWatchAddressAbsentIsolationAndRollback(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		seedRootConversation(t, b.store)
		s, e := OpenSession(b.store)
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close(bg)
		def := checkpointDef(t, DefinitionOptions{Kind: "watch.family", Scope: "session", Version: 1, Family: true,
			Initial:        func(JSON) (JSON, error) { return JSON{"n": 0}, nil },
			CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
		})
		key, other := "", "other"
		before, e := s.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		if w, ok, e := s.WatchDefinition(bg, def, 0, &key); e != nil || ok || w != nil {
			t.Fatal(w, ok, e)
		}
		after, e := s.Snapshot(bg)
		if e != nil || after.Seq != before.Seq || after.HighWater != before.HighWater {
			t.Fatal("absent watch created state", after, e)
		}
		checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, &key, nil); return e })
		w, ok, e := s.WatchDefinition(bg, def, 0, &key)
		if e != nil || !ok {
			t.Fatal(ok, e)
		}
		defer w.Stop()
		checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, &other, nil); return e })
		refusal := errors.New("rollback")
		if _, e = s.Commit(bg, func(tx *Tx) error {
			if e := tx.RetireDefinition(def, 0, &key); e != nil {
				return e
			}
			return refusal
		}); !errors.Is(e, refusal) {
			t.Fatal(e)
		}
		w.mu.Lock()
		pending := len(w.pending)
		id := w.id
		w.mu.Unlock()
		if pending != 0 || id == 0 {
			t.Fatal("unrelated address or rollback reached watch", pending, id)
		}
		checkpointCommit(t, s, func(tx *Tx) error { return tx.RetireDefinition(def, 0, &key) })
		frames := make(chan rawWatchFrame, 2)
		if e = startDetachedDocumentFrames(w, func(_ context.Context, f rawWatchFrame) error { frames <- f; return nil }); e != nil {
			t.Fatal(e)
		}
		f := watchReceive(t, frames)
		if !f.Retired || len(f.Ops) != 0 {
			t.Fatal(f)
		}
		if e = s.Close(bg); e != nil {
			t.Fatal(e)
		}
		watchReceive(t, w.Closed())
		if end, _ := w.End(); end.Reason != "session_closed" {
			t.Fatal(end)
		}
	})
}
