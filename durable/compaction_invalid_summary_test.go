package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCompactionRejectsStopWithToolCallWithoutPlacingSummary(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: goai.StopReasonStop, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "must not become summary"}, {Type: "toolCall", ID: "call", Name: "unknown", Arguments: map[string]any{}}}}}
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		zero := 0
		id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokensOverride: &zero})
		if err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "failed" || record.State.Outcome.Error.Message != "Summarization attempted to call a tool" || record.State.Outcome.Error.Detail.Value.(map[string]any)["reason"] != "model_error" {
			t.Fatal("accepted tool call summary", record.State.Outcome, record.State.Outcome.Error)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range state.Entries {
			if entry.Kind == "pi.compaction" {
				t.Fatal("invalid summary placed", entry)
			}
		}
	})
}
