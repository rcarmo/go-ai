package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestBusyPassiveWriteQueuesUntilAnswerBoundaryBeforeFollowUp(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered, release := make(chan struct{}), make(chan struct{})
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				go func() { close(entered); <-release; ch <- terminal("first answer"); close(ch) }()
			} else {
				ch <- terminal("second answer")
				close(ch)
			}
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation := root(t, h, ref)
		first, err := conversation.Submit(bg, Input{Content: "first"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		note := &Entry{Kind: "app.note", Edits: []ContextEdit{}, Value: JSON{"text": "passive"}}
		write, err := conversation.Submit(bg, Input{Type: "write", Entry: note, RequestID: "note"})
		if err != nil {
			t.Fatal(err)
		}
		follow, err := conversation.Submit(bg, Input{Content: "follow"})
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Submissions[write.ID()].Status != "pending" {
			t.Fatal("busy write settled early")
		}
		for _, entry := range snapshot.Entries {
			if entry.Kind == "app.note" {
				t.Fatal("busy write placed early")
			}
		}
		releaseTaskGate(release)
		waitSubmission(t, first)
		waitSubmission(t, write)
		waitSubmission(t, follow)
		snapshot, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var answerEntry, noteEntry, followEntry Entry
		for _, entry := range snapshot.Entries {
			if entry.Kind == "app.note" {
				noteEntry = entry
				continue
			}
			if isMessageEntry(entry) {
				var receipt MessageReceipt
				if err := decodeEntryMessage(entry, h.session.limits, &receipt); err != nil {
					t.Fatal(err)
				}
				if receipt.ContentText != nil && *receipt.ContentText == "follow" {
					followEntry = entry
				}
				for _, block := range receipt.Content {
					if block.Text == "first answer" {
						answerEntry = entry
					}
					if block.Text == "follow" {
						followEntry = entry
					}
				}
			}
		}
		if answerEntry.ID == 0 || noteEntry.ID == 0 || followEntry.ID == 0 || answerEntry.Seq == 0 || noteEntry.Seq != answerEntry.Seq || followEntry.Seq != answerEntry.Seq || answerEntry.Position >= noteEntry.Position || noteEntry.Position >= followEntry.Position || calls != 2 {
			t.Fatal("passive boundary must append answer, note, then follow-up in the same commit", answerEntry, noteEntry, followEntry, calls)
		}
	})
}
