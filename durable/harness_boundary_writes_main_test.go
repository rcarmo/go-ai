package durable

import (
	"cmp"
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"slices"
	"testing"
)

// Source-derived native Tx controls, not upstream test-body ports. Official
// v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5 inbox.ts applyBoundary:
// first is the first actual append, even if a later reset cuts it from context;
// stale numeric heads append nothing, and each write sees preceding head changes.
// Queue fixtures use canonical write records/drafts and real minted IDs; native
// Reset controls still exercise the separate historical reset path. These
// controls do not simulate model completion or enable scheduling.
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
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				h := openHarness(t, b.store, Options{})
				c := root(t, h, ModelRef{})
				entries, submissions := map[string]ID{}, map[string]ID{}
				if _, err := h.session.Commit(bg, func(tx *Tx) (any, error) {
					for _, text := range []string{"a", "b", "c"} {
						id, err := tx.mintID()
						if err != nil {
							return nil, err
						}
						entries[text] = id
						if err := tx.appendEntryRecord(Entry{ID: id, Conversation: c.ID(), Kind: "app.anchor", Value: JSON{}, Model: []MessageReceipt{userReceipt(text)}}); err != nil {
							return nil, err
						}
					}
					if tc.priorReset {
						id, err := appendResetEntry(tx, c.ID(), "baseline")
						if err != nil {
							return nil, err
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
						record, err := tx.CreateSubmission(SubmissionCreate{Conversation: c.ID(), Type: "write", Status: "queued"})
						if err != nil {
							return nil, err
						}
						submissions[write.text] = record.ID
						draft, err := inboxEntryDraft(Entry{Kind: "app.note", Head: entries[write.head], Model: []MessageReceipt{userReceipt(write.text)}}, tx.limits)
						if err != nil {
							return nil, err
						}
						items = append(items, JSON{"id": record.ID, "mode": "write", "entry": draft})
					}
					inbox, err := builtin(tx, c.ID(), "pi.inbox")
					if err != nil {
						return nil, err
					}
					return nil, inbox.Update(func(value JSON) error { value["items"] = items; return nil })
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
				value, err := h.session.Commit(bg, func(tx *Tx) (any, error) {
					var err error
					first, err = h.placeFinalBoundaryWrites(tx, c.ID())
					return nil, err
				})
				if err != nil || value != nil {
					t.Fatal("place real final boundary", value, err)
				}
				after := snap(t, b.store)
				seq := after.Seq
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
				if inboxes != 1 || after.Seq != before.Seq+1 || h.scheduler.enabled.Load() {
					t.Error("control must adopt exactly one unscheduled boundary with one inbox", inboxes, before.Seq, after.Seq, seq)
				}
			})
		})
	}
}

// Source-derived from pinned harness/inbox.ts applyBoundary: queued EntryDraft
// is authoritative, not any private payload attached to the submission record.
// Exercise normal admission and the real provider, not an alternate boundary.
func TestCanonicalInboxWritesAreBoundaryAuthority(t *testing.T) {
	for _, terminalRecord := range []bool{false, true} {
		t.Run(fmt.Sprint(terminalRecord), func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				ch <- terminal("answer")
				close(ch)
				return ch
			})
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			var write SubmissionRecord
			_, err = h.Commit(bg, func(tx *Tx) (any, error) {
				var err error
				status := "queued"
				var previous ID
				if terminalRecord {
					status = "done"
					previous = 999999
				}
				write, err = tx.CreateSubmission(SubmissionCreate{Conversation: c.ID(), Type: "write", Status: status, Entry: previous})
				if err != nil {
					return nil, err
				}
				inbox, err := tx.AcquireDocument(mountedDocumentDefinitions[2], c.ID(), nil, nil)
				if err != nil {
					return nil, err
				}
				return nil, inbox.Set(JSON{"items": []any{JSON{"id": write.ID, "mode": "write", "entry": JSON{"kind": "app.note", "data": nil, "model": []any{JSON{"role": "user", "content": []any{JSON{"type": "text", "text": "canonical note"}}, "timestamp": 1}}}}}})
			})
			if err != nil {
				t.Fatal(err)
			}
			submitted, err := c.Submit(bg, Input{Type: "input", Content: "hi"})
			if err != nil {
				t.Fatal(err)
			}
			if result := waitSubmission(t, submitted); result.Record.Status != "done" {
				t.Fatal(result)
			}
			page, err := c.Entries(bg, EntryQuery{Order: "ascending"}, 10, nil)
			if err != nil {
				t.Fatal(err)
			}
			var placed Entry
			for _, entry := range page.Items {
				if entry.Kind == "app.note" {
					placed = entry
				}
			}
			if placed.ID == 0 || !placed.HasData || placed.Data != nil || len(placed.Model) != 1 || len(placed.Model[0].Content) != 1 || placed.Model[0].Content[0].Text != "canonical note" {
				t.Fatal("canonical queued draft was not placed", page.Items)
			}
			record, found, err := h.session.Submission(bg, write.ID)
			if err != nil || !found || record.Status != "done" {
				t.Fatal(record, found, err)
			}
			if terminalRecord {
				if record.Entry != 999999 {
					t.Fatal("terminal record changed", record)
				}
			} else if record.Entry != placed.ID {
				t.Fatal("placement not recorded", record, placed)
			}
		})
	}
}

// Same source path: placeSubmission queues changes without reading the record;
// a missing queued target fails assembly after the host callback has finished.
func TestCanonicalInboxMissingWriteFailsAfterAdmissionCallback(t *testing.T) {
	h := openHarness(t, taskConformanceStoreV150(t), Options{})
	c := root(t, h, ModelRef{})
	_, err := h.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(mountedDocumentDefinitions[2], c.ID(), nil, nil)
		if err != nil {
			return nil, err
		}
		return nil, doc.Set(JSON{"items": []any{JSON{"id": 999999, "mode": "write", "entry": JSON{"kind": "app.note"}}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := false
	var admissionError error
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		_, admissionError = c.SubmitIn(tx, Input{Type: "input", Content: "hi"})
		if admissionError != nil {
			return nil, admissionError
		}
		finished = true
		return nil, nil
	})
	if !finished || admissionError != nil || err == nil || err.Error() != "Submission 999999 does not exist" {
		t.Fatal("missing write did not defer until assembly", finished, admissionError, err)
	}
}
