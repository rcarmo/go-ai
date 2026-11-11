package durable

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Source view.ts:364-371 at 81f3cd41 appends the entire publication, then
// trims oldest entries, then sets before. A final-value diff is not equivalent
// to that operation sequence, even though both can reconstruct the same view.
func TestMainTranscriptWindowEntryOperationSequence(t *testing.T) {
	for _, bounds := range []struct {
		name     string
		min, max int
	}{{"grow-trim", 2, 4}, {"equal", 2, 2}, {"single", 1, 1}} {
		t.Run(bounds.name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h, c := transcriptFixture(t, b)
				watch, err := detachedWatch(c, bg, transcriptBounds(bounds.min, bounds.max))
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
				type delivered struct {
					value ConversationViewCopy
					ops   []Operation
				}
				frames := make(chan delivered, 1)
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
						return fmt.Errorf("entry operations do not reconstruct view")
					}
					replay = wire
					frames <- delivered{value, ops}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				all := []ID{}
				for _, count := range []int{2, 2, 3, 1, 5} {
					previous := len(all)
					oldLen := len(initial.Entries)
					added := transcriptNotes(t, h, count)
					all = append(all, added...)
					var frame delivered
					select {
					case frame = <-frames:
					case <-time.After(3 * time.Second):
						t.Fatal("missing operation frame")
					}
					snapshot, err := h.session.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					inserted := make([]any, 0, count)
					for _, id := range added {
						record, err := conversationEntryObject(snapshot.Entries[id], h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						inserted = append(inserted, map[string]any(record))
					}
					want := []Operation{{"p", []any{"entries"}, oldLen, 0, inserted}}
					kept := oldLen + count
					before := initial.Before
					if kept > bounds.max {
						drop := kept - bounds.min
						before = all[previous-oldLen+drop-1]
						want = append(want, Operation{"p", []any{"entries"}, 0, drop, []any{}}, Operation{"s", []any{"before"}, before})
						kept = bounds.min
					}
					want, err = ownOperations(want, h.session.limits)
					if err != nil {
						t.Fatal(err)
					}
					oldWire, wireErr := dtoObject(initial, h.session.limits)
					if wireErr != nil {
						t.Fatal(wireErr)
					}
					if _, applyErr := ApplyOperations(oldWire, want, h.session.limits); applyErr != nil {
						t.Fatalf("reference tuples failed: %v", applyErr)
					}
					if len(frame.ops) != len(want) {
						t.Fatalf("publication %v: ops=%v; want=%v", added, frame.ops, want)
					}
					for i := range want {
						if !equalJSONValue([]any(frame.ops[i]), []any(want[i])) {
							t.Fatalf("publication %v: ops=%v; want=%v", added, frame.ops, want)
						}
					}
					transcriptIDs(t, frame.value, all[len(all)-kept:], before)
					initial = frame.value
				}
			})
		})
	}
}

func TestMainTranscriptWindowOperationNativeBounds(t *testing.T) {
	for _, mode := range []string{"members", "document-bytes", "insert-record-bytes"} {
		t.Run(mode, func(t *testing.T) {
			limits := DefaultLimits()
			before := ConversationViewCopy{Conversation: Conversation{ID: 100}, Docs: map[string]JSON{}, Entries: []Entry{}}
			for i := 1; i <= 14; i++ {
				before.Entries = append(before.Entries, Entry{ID: ID(i), Conversation: 100, Kind: "app.note", Value: JSON{"text": ""}})
			}
			publication := sessionPublication{}
			for i := 15; i <= 17; i++ {
				entry := Entry{ID: ID(i), Conversation: 100, Kind: "app.note", Value: JSON{"text": ""}}
				if mode == "insert-record-bytes" {
					legacyEntryValue(entry)["text"] = strings.Repeat("x", 180000)
				}
				publication.Tables = append(publication.Tables, Write{Op: "append-entry", Entry: &entry})
			}
			if mode == "members" {
				limits.MaxMembers = 16
			}
			if mode == "document-bytes" {
				body, err := dtoObject(before, limits)
				if err != nil {
					t.Fatal(err)
				}
				wire, err := encodeBounded(body, limits, limits.MaxDocumentBytes)
				if err != nil {
					t.Fatal(err)
				}
				limits.MaxDocumentBytes = len(wire) + 16
			}
			next, appended, drop, err := advanceConversationWindow(before, publication, limits, conversationWindowBounds{min: 1, max: 14})
			if err != nil {
				t.Fatal(err)
			}
			a, err := dtoObject(before, limits)
			if err != nil {
				t.Fatal(err)
			}
			z, err := dtoObject(next, limits)
			if err != nil {
				t.Fatal(err)
			}
			inserted := []any{}
			for _, entry := range appended {
				object, err := conversationEntryObject(entry, limits)
				if err != nil {
					t.Fatal(err)
				}
				inserted = append(inserted, map[string]any(object))
			}
			source := []Operation{{"p", []any{"entries"}, 14, 0, inserted}, {"p", []any{"entries"}, 0, 16, []any{}}, {"s", []any{"before"}, 16}}
			if _, err = ApplyOperations(a, source, limits); err == nil {
				t.Fatal("unrepresentable transient accepted")
			}
			ops, err := conversationWindowOperations(a, z, appended, drop, limits)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := ApplyOperations(a, ops, limits)
			if err != nil {
				t.Fatal(err)
			}
			if !equalJSONValue(replay, z) {
				t.Fatal("native bounded fallback lost publication state")
			}
			transcriptIDs(t, next, []ID{17}, 16)
		})
	}
}

func TestMainTranscriptWindowDocumentDiffRootKeepsEnvelope(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxMembers = 16
	beforeDocs, afterDocs := map[string]any{}, map[string]any{}
	for _, kind := range []string{"pi.live", "pi.inbox"} {
		old, next := map[string]any{}, map[string]any{}
		for i := 0; i < 12; i++ {
			key := fmt.Sprint(i)
			old[key] = false
			next[key] = true
		}
		beforeDocs[kind], afterDocs[kind] = old, next
	}
	before := JSON{"conversation": map[string]any{"id": 100}, "entries": []any{}, "before": 5, "docs": beforeDocs}
	after := JSON{"conversation": map[string]any{"id": 100}, "entries": []any{}, "before": 5, "docs": afterDocs}
	ops, err := conversationWindowOperations(before, after, nil, 0, limits)
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) != 1 || ops[0][0] != "r" {
		t.Fatalf("expected bounded full root, got %v", ops)
	}
	replay, err := ApplyOperations(before, ops, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !equalJSONValue(replay, after) {
		t.Fatalf("document diff root lost entries/before: %v", replay)
	}
}
