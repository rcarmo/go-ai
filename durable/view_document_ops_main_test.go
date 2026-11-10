package durable

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func assertConversationOperations(t *testing.T, got, want []Operation, limits Limits) {
	t.Helper()
	owned, err := ownOperations(want, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(owned) {
		t.Fatalf("ops=%v; want=%v", got, owned)
	}
	for i := range got {
		if !equalJSONValue([]any(got[i]), []any(owned[i])) {
			t.Fatalf("ops=%v; want=%v", got, owned)
		}
	}
}

// Source view.ts:340-352 at 81f3cd41 uses published operations, not a diff
// between final values, and treats incarnation/version changes as replacements.
func TestMainConversationDocumentOperationTuples(t *testing.T) {
	for _, mode := range []string{"replace-with-entry", "explicit-revert", "reincarnate-equal", "version-equal", "retire"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				watch, err := detachedWatch(c, bg)
				if err != nil {
					t.Fatal(err)
				}
				defer watch.Stop()
				initial, err := watch.Value()
				if err != nil {
					t.Fatal(err)
				}
				replay, err := dtoObject(initial, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				type delivery struct {
					value ConversationViewCopy
					ops   []Operation
				}
				frames := make(chan delivery, 1)
				if err = watch.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
					next, err := ApplyOperations(replay, ops, h.session.limits)
					if err != nil {
						return err
					}
					wire, err := dtoObject(value, h.session.limits)
					if err != nil {
						return err
					}
					if !equalJSONValue(next, wire) {
						return fmt.Errorf("document tuple replay mismatch")
					}
					replay = wire
					owned, err := ownOperations(ops, h.session.limits)
					if err != nil {
						return err
					}
					frames <- delivery{value, owned}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				var entryID ID
				_, err = h.session.Commit(bg, func(tx *Tx) error {
					live, err := builtin(tx, 1, "pi.live")
					if err != nil {
						return err
					}
					switch mode {
					case "replace-with-entry":
						if err = live.Set(JSON{"detail": "replacement"}); err != nil {
							return err
						}
						entryID, err = tx.MintID()
						if err != nil {
							return err
						}
						return tx.AppendEntry(Entry{ID: entryID, Conversation: 1, Kind: "app.note", Value: JSON{}})
					case "explicit-revert":
						return live.ApplyOperations([]Operation{{"s", []any{"marker"}, true}, {"d", []any{"marker"}}})
					case "reincarnate-equal":
						if err = live.Retire(); err != nil {
							return err
						}
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						_, err = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: 1, Kind: "pi.live", Version: 1, Value: initial.Docs["pi.live"]})
						return err
					case "version-equal":
						record, err := live.record()
						if err != nil {
							return err
						}
						record.Version++
						return tx.stage(Write{Op: "put-document", Document: &record})
					case "retire":
						return live.Retire()
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				var frame delivery
				select {
				case frame = <-frames:
				case <-time.After(3 * time.Second):
					end, ended := watch.End()
					t.Fatalf("missing document frame: ended=%v end=%v", ended, end)
				}
				path := []any{"docs", "pi.live"}
				var want []Operation
				switch mode {
				case "replace-with-entry":
					snapshot, err := h.session.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					entry, err := conversationEntryObject(snapshot.Entries[entryID], h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					want = []Operation{{"s", path, map[string]any{"detail": "replacement"}}, {"p", []any{"entries"}, 0, 0, []any{map[string]any(entry)}}}
				case "explicit-revert":
					want = []Operation{{"s", []any{"docs", "pi.live", "marker"}, true}, {"d", []any{"docs", "pi.live", "marker"}}}
				case "reincarnate-equal":
					want = []Operation{{"d", path}, {"s", path, map[string]any(initial.Docs["pi.live"])}}
				case "version-equal":
					want = []Operation{{"s", path, map[string]any(initial.Docs["pi.live"])}}
				case "retire":
					want = []Operation{{"d", path}}
				}
				assertConversationOperations(t, frame.ops, want, h.session.limits)
			})
		})
	}
}

// A previously reserved ID can sort before the retired incarnation. Source's
// ID guard must not delete the new mount when the old retirement arrives last.
func TestMainConversationDocumentOlderReplacementID(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := openHarness(t, b.store, Options{})
		c, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		var reserved ID
		if _, err = h.session.Commit(bg, func(tx *Tx) error { var err error; reserved, err = tx.MintID(); return err }); err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) error { return initializeBuiltins(tx, 1) }); err != nil {
			t.Fatal(err)
		}
		watch, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		frames := make(chan []Operation, 2)
		if err = watch.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
			if value.Docs["pi.live"]["detail"] != "new" {
				return fmt.Errorf("late old retirement removed new mount")
			}
			frames <- ops
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			old, err := builtin(tx, 1, "pi.live")
			if err != nil {
				return err
			}
			if old.id <= reserved {
				return fmt.Errorf("fixture did not reserve an earlier ID")
			}
			if err = old.Retire(); err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: reserved, Scope: "conversation", Owner: 1, Kind: "pi.live", Version: 1, Value: JSON{"detail": "new"}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		receive := func() []Operation {
			select {
			case ops := <-frames:
				return ops
			case <-time.After(3 * time.Second):
				t.Fatal("missing incarnation frame")
				return nil
			}
		}
		assertConversationOperations(t, receive(), []Operation{{"s", []any{"docs", "pi.live"}, map[string]any{"detail": "new"}}}, h.session.limits)
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			current, err := tx.Document(reserved)
			if err != nil {
				return err
			}
			return current.ApplyOperations([]Operation{{"s", []any{"later"}, true}})
		}); err != nil {
			t.Fatal(err)
		}
		assertConversationOperations(t, receive(), []Operation{{"s", []any{"docs", "pi.live", "later"}, true}}, h.session.limits)
	})
}

func TestMainConversationDocumentOperationOwnership(t *testing.T) {
	for _, versionChange := range []bool{false, true} {
		t.Run(fmt.Sprintf("version-change-%v", versionChange), func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				first, err := detachedWatch(c, bg)
				if err != nil {
					t.Fatal(err)
				}
				defer first.Stop()
				peer, err := detachedWatch(c, bg)
				if err != nil {
					t.Fatal(err)
				}
				defer peer.Stop()
				if _, err = h.session.Commit(bg, func(tx *Tx) error {
					live, err := builtin(tx, 1, "pi.live")
					if err != nil {
						return err
					}
					if versionChange {
						record, err := live.record()
						if err != nil {
							return err
						}
						record.Version++
						record.Value = JSON{"detail": "committed"}
						return tx.stage(Write{Op: "put-document", Document: &record})
					}
					return live.Set(JSON{"detail": "committed"})
				}); err != nil {
					t.Fatal(err)
				}
				mutated := make(chan struct{})
				if err = first.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
					ops[0][1].([]any)[1] = "escaped-path"
					ops[0][2].(map[string]any)["detail"] = "escaped-op"
					value.Docs["pi.live"]["detail"] = "escaped-value"
					close(mutated)
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				select {
				case <-mutated:
				case <-time.After(3 * time.Second):
					t.Fatal("missing first callback")
				}
				own, err := first.Value()
				if err != nil || own.Docs["pi.live"]["detail"] != "committed" {
					t.Fatal("operation mutation escaped to private head", own, err)
				}
				frames := make(chan []Operation, 1)
				if err = peer.Start(func(_ context.Context, value ConversationViewCopy, ops []Operation) error {
					if value.Docs["pi.live"]["detail"] != "committed" {
						return fmt.Errorf("peer value escaped")
					}
					frames <- ops
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				select {
				case ops := <-frames:
					assertConversationOperations(t, ops, []Operation{{"s", []any{"docs", "pi.live"}, map[string]any{"detail": "committed"}}}, h.session.limits)
				case <-time.After(3 * time.Second):
					t.Fatal("peer operations escaped")
				}
			})
		})
	}
}

func TestMainConversationDocumentAbandonedPreparation(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		watch, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		state, err := h.session.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var record Document
		for _, doc := range state.Documents {
			if doc.Owner == 1 && doc.Kind == "pi.live" && !doc.Retired {
				record = doc
			}
		}
		if record.ID == 0 {
			t.Fatal("missing live document")
		}
		record.Version++
		prepared := watch.preparePublication(bg, CommitPublication{Documents: []DocumentChange{{Record: record, Version: record.Version, Value: record.Value}}}, make(chan struct{}))
		if prepared == nil || prepared.incarnations["pi.live"].version != record.Version {
			t.Fatal("missing prepared incarnation")
		}
		watch.mu.Lock()
		version := watch.incarnations["pi.live"].version
		watch.mu.Unlock()
		if version == record.Version {
			t.Fatal("unreceipted incarnation installed")
		}
		frames := make(chan []Operation, 1)
		if err = watch.Start(func(_ context.Context, _ ConversationViewCopy, ops []Operation) error { frames <- ops; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			live, err := tx.Document(record.ID)
			if err != nil {
				return err
			}
			return live.ApplyOperations([]Operation{{"s", []any{"marker"}, true}})
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case ops := <-frames:
			assertConversationOperations(t, ops, []Operation{{"s", []any{"docs", "pi.live", "marker"}, true}}, h.session.limits)
		case <-time.After(3 * time.Second):
			t.Fatal("missing post-abandonment frame")
		}
	})
}

func TestMainConversationDocumentTransientViewBudget(t *testing.T) {
	limits := DefaultLimits()
	view := ConversationViewCopy{Conversation: Conversation{ID: 1}, Entries: []Entry{{ID: 2, Conversation: 1, Kind: "app.note", Value: JSON{"text": strings.Repeat("x", 128)}}}, Docs: map[string]JSON{"pi.live": {}}}
	before, err := dtoObject(view, limits)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := encodeBounded(before, limits, limits.MaxDocumentBytes)
	if err != nil {
		t.Fatal(err)
	}
	limits.MaxDocumentBytes = len(wire) + 32
	documentOps := []Operation{{"s", []any{"docs", "pi.live", "temporary"}, strings.Repeat("y", 100)}, {"d", []any{"docs", "pi.live", "temporary"}}}
	if _, err = ApplyOperations(before, documentOps, limits); err == nil {
		t.Fatal("fixture did not exceed the full-view transient budget")
	}
	ops, err := conversationWindowOperations(before, before, nil, 0, limits, documentOps)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := ApplyOperations(before, ops, limits)
	if err != nil || !equalJSONValue(replay, before) {
		t.Fatal("bounded document fallback failed", replay, err)
	}
}

// A watch-local fault budget forces an empty bounded diff for equal-value
// reincarnations. Their receipted witness must still advance without a callback.
func TestMainConversationDocumentBoundedEmptyDiffAdoptsWitness(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h, c := transcriptFixture(t, b)
		watch, err := detachedWatch(c, bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		state, err := h.session.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		expected := map[string]ID{}
		watch.limits.MaxMembers = 7 // eight retire/create operations cannot fit
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			for _, kind := range []string{"pi.live", "pi.inbox", "pi.usage", "pi.provider"} {
				var old Document
				for _, doc := range state.Documents {
					if doc.Kind == kind && doc.Owner == 1 && !doc.Retired {
						old = doc
					}
				}
				if old.ID == 0 {
					return fmt.Errorf("missing %s", kind)
				}
				handle, err := tx.Document(old.ID)
				if err != nil {
					return err
				}
				if err = handle.Retire(); err != nil {
					return err
				}
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				expected[kind] = id
				if _, err = tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: 1, Kind: kind, Version: old.Version, Value: old.Value, History: old.History, Fork: old.Fork}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		watch.sub.mu.Lock()
		pending, bytes := len(watch.sub.pending), watch.sub.bytes
		watch.sub.mu.Unlock()
		if pending != 0 || bytes != 0 {
			t.Fatal("empty diff consumed delivery capacity", pending, bytes)
		}
		watch.mu.Lock()
		for kind, id := range expected {
			if watch.incarnations[kind].id != id {
				t.Errorf("bounded empty diff lost %s witness: got %d want %d", kind, watch.incarnations[kind].id, id)
			}
		}
		watch.mu.Unlock()
		watch.limits = h.session.limits
		frames := make(chan []Operation, 1)
		if err = watch.Start(func(_ context.Context, _ ConversationViewCopy, ops []Operation) error { frames <- ops; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err = h.session.Commit(bg, func(tx *Tx) error {
			current, err := tx.Document(expected["pi.live"])
			if err != nil {
				return err
			}
			return current.ApplyOperations([]Operation{{"s", []any{"later"}, true}})
		}); err != nil {
			t.Fatal(err)
		}
		select {
		case ops := <-frames:
			assertConversationOperations(t, ops, []Operation{{"s", []any{"docs", "pi.live", "later"}, true}}, h.session.limits)
		case <-time.After(3 * time.Second):
			t.Fatal("missing bounded-diff successor")
		}
	})
}
