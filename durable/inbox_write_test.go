package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestBusyPassiveWriteQueuesUntilAnswerBoundaryBeforeFollowUp(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
		note := &Entry{Kind: "app.note", Value: JSON{"text": "passive"}}
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
		var answerSeq, noteSeq, followSeq uint64
		for _, entry := range snapshot.Entries {
			if entry.Kind == "app.note" {
				noteSeq = entry.Seq
				continue
			}
			if entry.Kind == "message" {
				var receipt MessageReceipt
				if err := fromObject(entry.Value, &receipt, h.session.limits); err != nil {
					t.Fatal(err)
				}
				for _, block := range receipt.Content {
					if block.Text == "first answer" {
						answerSeq = entry.Seq
					}
					if block.Text == "follow" {
						followSeq = entry.Seq
					}
				}
			}
		}
		if noteSeq != answerSeq || followSeq <= noteSeq || calls != 2 {
			t.Fatal("passive boundary order", answerSeq, noteSeq, followSeq, calls)
		}
	})
}
