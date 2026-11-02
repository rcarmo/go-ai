package durable

import (
	"cmp"
	"slices"
	"testing"
)

// Source-derived native Tx controls, not upstream test-body ports. Official
// v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5 inbox.ts applyBoundary:
// first is the first actual append, even if a later reset cuts it from context;
// stale numeric heads append nothing, and each write sees preceding head changes.
// Queue fixtures use real minted IDs and the native Submit/Reset inbox shapes;
// they deliberately do not simulate model completion or enable scheduling.
func TestMainBoundaryWritesFirstAndSequentialHeads(t *testing.T) {
	type queuedWrite struct {
		text, head   string
		reset, stale bool
	}
	for _, tc := range []struct {
		name                string
		priorReset          bool
		queue               []queuedWrite
		appends, context    []string
		first, head, target string
	}{
		{
			name:    "write-reset-stale-write",
			queue:   []queuedWrite{{text: "W0"}, {text: "R", reset: true}, {text: "S", head: "a", stale: true}, {text: "W1"}},
			appends: []string{"W0", "R", "W1"}, context: []string{"R", "W1"},
			first: "W0", head: "R", target: "R",
		},
		{
			name: "stale-only", priorReset: true,
			queue:   []queuedWrite{{text: "S", head: "a", stale: true}},
			context: []string{"baseline"}, head: "baseline", target: "baseline",
		},
		{
			name:    "numeric-head-progression",
			queue:   []queuedWrite{{text: "N1", head: "b"}, {text: "S1", head: "a", stale: true}, {text: "N2", head: "b"}, {text: "N3", head: "c"}, {text: "S2", head: "b", stale: true}},
			appends: []string{"N1", "N2", "N3"}, context: []string{"N3", "c"},
			first: "N1", head: "N3", target: "c",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				h := openHarness(t, b.store, Options{})
				c := root(t, h, ModelRef{})
				entries, submissions := map[string]ID{}, map[string]ID{}
				if _, err := h.session.Commit(bg, func(tx *Tx) error {
					for _, text := range []string{"a", "b", "c"} {
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						entries[text] = id
						if err := tx.AppendEntry(Entry{ID: id, Conversation: c.ID(), Kind: "app.anchor", Value: JSON{}, Model: []MessageReceipt{userReceipt(text)}}); err != nil {
							return err
						}
					}
					if tc.priorReset {
						id, err := appendResetEntry(tx, c.ID(), "baseline")
						if err != nil {
							return err
						}
						entries["baseline"] = id
					}
					items := []any{}
					for _, write := range tc.queue {
						if write.reset {
							// Native Reset is a separate inbox item, unlike upstream's
							// head:"self" write; its entry ID is minted on placement.
							items = append(items, JSON{"mode": "reset", "content": write.text})
							continue
						}
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						submissions[write.text] = id
						draft, err := dtoObject(Entry{Kind: "app.note", Value: JSON{"text": write.text}, Head: entries[write.head], Model: []MessageReceipt{userReceipt(write.text)}}, tx.limits)
						if err != nil {
							return err
						}
						if err := tx.PutSubmission(Submission{ID: id, Conversation: c.ID(), Type: "write", Status: "pending", Value: JSON{"entry": map[string]any(draft)}}); err != nil {
							return err
						}
						items = append(items, JSON{"id": id, "mode": "write"})
					}
					inbox, err := builtin(tx, c.ID(), "pi.inbox")
					if err != nil {
						return err
					}
					return inbox.Update(func(value JSON) error { value["items"] = items; return nil })
				}); err != nil {
					t.Fatal("native queue control", err)
				}
				before := snap(t, b.store)
				for text, id := range submissions {
					if sub := publicSubmission(before.Submissions[id]); sub.Status != "queued" || sub.Entry != 0 || sub.Answer != 0 {
						t.Fatal("fixture must be queued and unplaced", text, sub)
					}
				}
				var first ID
				seq, err := h.session.Commit(bg, func(tx *Tx) error {
					var err error
					first, err = h.placeFinalBoundaryWrites(tx, c.ID())
					return err
				})
				if err != nil {
					t.Fatal("place real final boundary", err)
				}
				after := snap(t, b.store)
				var appended []Entry
				for id, entry := range after.Entries {
					if _, existed := before.Entries[id]; !existed {
						appended = append(appended, entry)
					}
				}
				slices.SortFunc(appended, func(a, b Entry) int { return cmp.Compare(a.Position, b.Position) })
				var texts []string
				for _, entry := range appended {
					if entry.Conversation != c.ID() || entry.Seq != seq || len(entry.Model) != 1 || len(entry.Model[0].Content) != 1 {
						t.Fatal("unexpected persisted boundary entry", entry)
					}
					text := entry.Model[0].Content[0].Text
					texts = append(texts, text)
					entries[text] = entry.ID
				}
				if !slices.Equal(texts, tc.appends) || len(after.Entries) != len(before.Entries)+len(tc.appends) {
					t.Fatalf("boundary append order = %v; want %v (stale writes must append nothing)", texts, tc.appends)
				}
				if first != entries[tc.first] {
					t.Errorf("first = %d; want %q entry %d, not the latest head or historical entry", first, tc.first, entries[tc.first])
				}
				for _, write := range tc.queue {
					if write.reset {
						entry := after.Entries[entries[write.text]]
						if entry.Kind != "pi.reset" || entry.Head != entry.ID {
							t.Fatal("reset must place a real self-head marker", entry)
						}
						continue
					}
					sub := after.Submissions[submissions[write.text]]
					status := publicSubmission(sub)
					if write.stale {
						if sub.Status != "failed" || status.Status != "unanswered" || status.Reason != "stale" || status.Entry != 0 || status.Answer != 0 {
							t.Error("stale numeric head must settle without placement", write.text, sub, status)
						}
					} else if sub.Status != "done" || status.Entry != entries[write.text] || status.Entry == 0 || status.Answer != 0 || after.Entries[status.Entry].Head != entries[write.head] {
						t.Error("valid write must settle at its own entry with its original head", write.text, sub, status)
					}
				}
				inboxes := 0
				for _, doc := range after.Documents {
					if doc.Owner == c.ID() && doc.Kind == "pi.inbox" && !doc.Retired {
						items, ok := doc.Value["items"].([]any)
						if !ok || len(items) != 0 {
							t.Error("placed and stale writes must leave the inbox", doc.Value)
						}
						inboxes++
					}
				}
				view, err := c.ContextView(bg, 0)
				if err != nil {
					t.Fatal(err)
				}
				var contextTexts []string
				for _, message := range view.Messages {
					for _, block := range message.Content {
						contextTexts = append(contextTexts, block.Text)
					}
				}
				if view.Head == nil || view.Head.ID != entries[tc.head] || view.Head.Head != entries[tc.target] || !slices.Equal(contextTexts, tc.context) {
					t.Errorf("active head/context = %+v / %v; want head %q targeting %q / %v", view.Head, contextTexts, tc.head, tc.target, tc.context)
				}
				if inboxes != 1 || after.Seq != before.Seq+1 || after.Seq != seq || h.scheduler.enabled.Load() {
					t.Error("control must adopt exactly one unscheduled boundary with one inbox", inboxes, before.Seq, after.Seq, seq)
				}
			})
		})
	}
}
