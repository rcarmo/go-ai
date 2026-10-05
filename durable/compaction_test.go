package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestManualCompactionPinsCutUsageAndReopensWithoutReplay(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
			calls.Add(1)
			if !strings.Contains(input.SystemPrompt, "Summarise") {
				t.Error("not summary request", input.SystemPrompt)
			}
			if len(input.Messages) != 1 || !strings.Contains(input.Messages[0].Content[0].Text, "old question") || strings.Contains(input.Messages[0].Content[0].Text, "recent question") {
				t.Error("wrong pinned summary range", input.Messages)
			}
			if opts.MaxTokens == nil || *opts.MaxTokens != 100 {
				t.Error("summary options", opts)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("Remember the earlier decision.")
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		_, err := conversation.Commit(bg, func(tx *Tx) error {
			for _, message := range []MessageReceipt{userReceipt("old question"), {Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "old answer"}}, StopReason: goai.StopReasonStop}, userReceipt("recent question")} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(message, tx.limits)
				if err != nil {
					return err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("recent question"))), MaxTokens: 100})
		if err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		view, err := conversation.ContextView(bg, 0)
		if err != nil || len(view.Messages) != 2 || !strings.Contains(view.Messages[0].Content[0].Text, "Remember") || view.Messages[1].Content[0].Text != "recent question" {
			t.Fatal("compacted context", view, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		reopened, err := second.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		again, err := reopened.ContextView(bg, 0)
		if err != nil || len(again.Messages) != 2 || calls.Load() != 1 {
			t.Fatal("compaction replay", again, err, calls.Load())
		}
		if result := waitPublicTask(t, second, id); result.State.Outcome.Status != "completed" || calls.Load() != 1 {
			t.Fatal(result)
		}
	})
}

func TestManualCompactionBusyQueuesSummaryUntilBoundary(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var summaries, requests atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if strings.Contains(input.SystemPrompt, "Summarise") {
				summaries.Add(1)
				ch <- terminal("summary while busy")
				close(ch)
			} else {
				requests.Add(1)
				go func() { close(entered); <-release; ch <- terminal("busy answer"); close(ch) }()
			}
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation := root(t, h, ref)
		_, err := conversation.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old question", "old answer", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				wire, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: wire}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "busy question"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("busy question")))})
		if err != nil {
			t.Fatal("busy compaction rejected", err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		raw := record.State.Outcome.Result.Value.(map[string]any)
		number, ok := exactNumber(raw["submissionId"])
		if !ok {
			t.Fatal("missing placement submission", raw)
		}
		placement := ID(number.Num().Uint64())
		snapshot, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.Submissions[placement].Status != "pending" {
			t.Fatal("busy summary placed early", snapshot.Submissions[placement])
		}
		for _, entry := range snapshot.Entries {
			if entry.Kind == "pi.compaction" {
				t.Fatal("summary changed busy transcript")
			}
		}
		releaseTaskGate(release)
		waitSubmission(t, sub)
		write, err := h.Submission(bg, placement)
		if err != nil {
			t.Fatal(err)
		}
		if settled := waitSubmission(t, write); settled.Submission.Status != "done" {
			t.Fatal(settled)
		}
		view, err := conversation.ContextView(bg, 0)
		if err != nil || view.Head == nil || view.Head.Kind != "pi.compaction" {
			t.Fatal(view, err)
		}
		if summaries.Load() != 1 || requests.Load() != 1 {
			t.Fatal("passive summary triggered request", summaries.Load(), requests.Load())
		}
	})
}
func TestQueuedCompactionHeadOlderThanResetSettlesStale(t *testing.T) {
	store, _ := NewMemory()
	h := openHarness(t, store, Options{})
	// Raw native commit proves same-commit head progression, independently of
	// model timing: a queued summary must not bring pre-reset history back.
	var submission ID
	_, err := h.session.Commit(bg, func(tx *Tx) error {
		old, err := tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: old, Conversation: 1, Kind: "app.old", Value: JSON{}}); err != nil {
			return err
		}
		if err = initializeBuiltins(tx, 1); err != nil {
			return err
		}
		if err = appendReset(tx, 1, "new range"); err != nil {
			return err
		}
		submission, err = admitCompactionWrite(tx, 1, 999, Entry{Kind: "pi.compaction", Head: old, Value: JSON{}, Model: []MessageReceipt{userReceipt("obsolete summary")}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if state.Submissions[submission].Status != "failed" || state.Submissions[submission].Value["errorCode"] != "stale" {
		t.Fatal(state.Submissions[submission])
	}
	for _, entry := range state.Entries {
		if entry.Kind == "pi.compaction" {
			t.Fatal("stale summary restored history")
		}
	}
}
