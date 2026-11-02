package durable

import (
	"errors"
	"testing"
)

// Native source-derived rollback control, not a full upstream test-body port:
// official v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5 inbox.ts
// applyBoundary. A rejected candidate must not mutate the adopted submission
// through readtx.current's shared Value, whether appending or classifying stale.
// Session admission controls keep scheduling disabled; all IDs are really minted
// and all queued passive writes are admitted by the public Submit path.
func TestMainBoundaryPassiveWriteRollback(t *testing.T) {
	for _, stale := range []bool{false, true} {
		name := "append"
		if stale {
			name = "stale-head"
		}
		t.Run(name, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				h := openHarness(t, b.store, Options{})
				c := root(t, h, ModelRef{})
				var owner, input, anchor ID
				// Admit a valid native queued generation and place its actual input
				// through the production boundary without enabling the scheduler.
				if _, err := h.session.Commit(bg, func(tx *Tx) error {
					var err error
					input, err = tx.MintID()
					if err != nil {
						return err
					}
					owner, err = tx.MintID()
					if err != nil {
						return err
					}
					if err := tx.PutSubmission(Submission{ID: input, Conversation: c.ID(), Type: "follow-up", Status: "pending", Value: JSON{"content": "A"}}); err != nil {
						return err
					}
					cp, err := dtoObject(generationCheckpoint{Phase: "queued", Submission: input, Input: "A"}, tx.limits)
					if err != nil {
						return err
					}
					if err := tx.PutTask(Task{ID: owner, Conversation: c.ID(), Kind: "pi.generation", Status: "pending", Checkpoint: cp}); err != nil {
						return err
					}
					inbox, err := builtin(tx, c.ID(), "pi.inbox")
					if err != nil {
						return err
					}
					if err := inbox.Update(func(value JSON) error {
						value["items"] = []any{JSON{"id": input, "mode": "followUp", "input": JSON{"content": "A"}}}
						return nil
					}); err != nil {
						return err
					}
					return h.startQueuedRunBoundary(tx, c.ID(), 0, false)
				}); err != nil {
					t.Fatal("admit unscheduled input", err)
				}
				placed := snap(t, b.store)
				anchor = publicSubmission(placed.Submissions[input]).Entry
				if anchor == 0 || placed.Entries[anchor].ID != anchor || h.scheduler.enabled.Load() {
					t.Fatal("fixture must own a real placed input with scheduling disabled")
				}
				draft := Entry{Kind: "app.note", Value: JSON{"text": "W"}, Model: []MessageReceipt{userReceipt("W")}}
				if stale {
					draft.Head = anchor
				}
				write, err := c.Submit(bg, Input{Type: "write", Entry: &draft})
				if err != nil {
					t.Fatal("queue actual passive write", err)
				}
				// Explicit end-boundary control, not a simulated model completion:
				// release the real live.run before taking the rollback baseline.
				// The stale lane advances the real context head after W was queued.
				if _, err := h.session.Commit(bg, func(tx *Tx) error {
					live, err := builtin(tx, c.ID(), "pi.live")
					if err != nil {
						return err
					}
					if err := live.Update(func(value JSON) error { delete(value, "run"); return nil }); err != nil {
						return err
					}
					if stale {
						return appendReset(tx, c.ID(), "new context head")
					}
					return nil
				}); err != nil {
					t.Fatal("prepare explicit boundary control", err)
				}
				before := snap(t, b.store)
				queued := before.Submissions[write.ID()]
				status := publicSubmission(queued)
				if queued.Status != "pending" || status.Status != "queued" || status.Entry != 0 || status.Answer != 0 || h.scheduler.enabled.Load() {
					t.Fatal("W must really be queued, unplaced and unscheduled", queued, status)
				}
				if got, err := passiveHeadStale(before, c.ID(), queued.Value, h.session.limits); err != nil || got != stale {
					t.Fatal("actual queued head must select the intended branch", got, err)
				}
				requireInbox := func(state Snapshot, count int) {
					t.Helper()
					found := 0
					for _, doc := range state.Documents {
						if doc.Owner != c.ID() || doc.Kind != "pi.inbox" || doc.Retired {
							continue
						}
						found++
						items, ok := doc.Value["items"].([]any)
						if !ok || len(items) != count || count == 1 && submissionEntryID(generationObject(items[0])["id"]) != write.ID() {
							t.Fatal("unexpected actual inbox", doc.Value)
						}
					}
					if found != 1 {
						t.Fatal("expected exactly one inbox", found)
					}
				}
				requireInbox(before, 1)

				refusal := &StorageRejected{Reason: "forced rollback after passive boundary classification"}
				var candidateEntry ID
				classified := false
				seq, err := h.session.Commit(bg, func(tx *Tx) error {
					if err := h.startQueuedRunBoundary(tx, c.ID(), owner, false); err != nil {
						return err
					}
					candidate, err := tx.current()
					if err != nil {
						return err
					}
					sub := candidate.Submissions[write.ID()]
					candidateEntry = publicSubmission(sub).Entry
					if stale {
						if sub.Status != "failed" || sub.Value["errorCode"] != "stale" || candidateEntry != 0 || len(candidate.Entries) != len(before.Entries) {
							t.Fatal("rejection must follow actual stale classification, without appending W", sub)
						}
					} else if sub.Status != "done" || candidateEntry == 0 || candidate.Entries[candidateEntry].Kind != "app.note" || candidate.Entries[candidateEntry].Value["text"] != "W" || len(candidate.Entries) != len(before.Entries)+1 {
						t.Fatal("rejection must follow actual W append and settlement", sub)
					}
					requireInbox(candidate, 0)
					classified = true
					return refusal
				})
				var rejected *StorageRejected
				if !classified || seq != 0 || !errors.As(err, &rejected) || rejected != refusal {
					t.Fatal("must return the forced callback refusal after the real boundary", classified, seq, err)
				}
				for _, source := range []struct {
					name string
					read func() (Snapshot, error)
				}{
					{"session", func() (Snapshot, error) { return h.session.Snapshot(bg) }},
					{"storage", func() (Snapshot, error) { return b.store.Snapshot(bg) }},
				} {
					after, err := source.read()
					if err != nil {
						t.Fatal(source.name, err)
					}
					sub := after.Submissions[write.ID()]
					_, placedLeak := sub.Value["placedEntry"]
					_, errorLeak := sub.Value["errorCode"]
					if sub.Status != queued.Status || placedLeak || errorLeak || !equalJSONValue(sub.Value, queued.Value) {
						t.Error(source.name, "rejected boundary leaked submission state", sub)
					}
					if _, adopted := after.Entries[candidateEntry]; candidateEntry != 0 && adopted {
						t.Error(source.name, "rejected boundary adopted its appended entry", candidateEntry)
					}
					requireInbox(after, 1)
					// MintID reservations are intentionally not transactional. Only
					// that allocator fence may advance; every adopted table and
					// document revision must remain exactly at the prior sequence.
					if after.HighWater < before.HighWater {
						t.Fatal(source.name, "allocator moved backwards")
					}
					after.HighWater = before.HighWater
					if !equalSnapshot(before, after) {
						t.Error(source.name, "rejected boundary changed the adopted snapshot")
					}
				}
				// Positive retry control: the same retained queue can still adopt,
				// with a new real entry in the append lane, or no entry when stale.
				if _, err := h.session.Commit(bg, func(tx *Tx) error {
					return h.startQueuedRunBoundary(tx, c.ID(), owner, false)
				}); err != nil {
					t.Fatal("positive boundary retry", err)
				}
				after := snap(t, b.store)
				settled := after.Submissions[write.ID()]
				entry := publicSubmission(settled).Entry
				if stale {
					if settled.Status != "failed" || settled.Value["errorCode"] != "stale" || entry != 0 || len(after.Entries) != len(before.Entries) {
						t.Error("positive stale control", settled)
					}
				} else if settled.Status != "done" || entry == 0 || entry == candidateEntry || after.Entries[entry].Kind != "app.note" || after.Entries[entry].Value["text"] != "W" || len(after.Entries) != len(before.Entries)+1 {
					t.Error("positive append control", settled)
				}
				requireInbox(after, 0)
				if after.Seq != before.Seq+1 || h.scheduler.enabled.Load() {
					t.Error("only the positive boundary may adopt, without enabling scheduling", before.Seq, after.Seq)
				}
			})
		})
	}
}
