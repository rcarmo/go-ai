package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundThresholdCompactionDoesNotBlockAnswerOrOrdinaryIdle(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var summaries, requests atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if strings.Contains(input.SystemPrompt, "Summarise") {
				summaries.Add(1)
				go func() { close(entered); <-release; ch <- terminal("background summary"); close(ch) }()
			} else {
				requests.Add(1)
				// Witness selection before the answer changes the keep-token tail.
				// The answer still returns while the actual summary host is held.
				go func() {
					defer close(ch)
					select {
					case <-entered:
						ch <- terminal("answer without waiting")
					case <-ctx.Done():
					}
				}()
			}
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Compaction: CompactionPolicy{Enabled: true, TriggerTokens: 1000, BackgroundTokens: 950, KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("new question")))}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = conversation.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{strings.Repeat("earlier context ", 20), "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
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
		sub, err := conversation.Submit(bg, Input{Content: "new question"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		result := waitSubmission(t, sub)
		if result.Submission.Status != "done" || requests.Load() != 1 {
			t.Fatal("background compaction blocked answer", result)
		}
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := conversation.WaitForIdle(deadline); err != nil {
			t.Fatal("ordinary idle crossed background", err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		statusFound := false
		for _, document := range state.Documents {
			if document.Kind == "pi.live" && document.Owner == conversation.ID() {
				items, _ := document.Value["compactions"].([]any)
				statusFound = len(items) == 1
				if document.Value["run"] != nil {
					t.Fatal("settled run retained", document)
				}
			}
		}
		if !statusFound {
			t.Fatal("answer cleanup erased background compaction")
		}
		var id ID
		for _, task := range state.Tasks {
			if task.Kind == "task.pi.compaction" {
				id = task.ID
				record, err := CanonicalTask(task, h.session.limits)
				if err != nil || !record.Background || record.Owner != 0 {
					t.Fatal("background compaction ownership", record, err)
				}
			}
		}
		if id == 0 {
			t.Fatal("missing background compaction")
		}
		releaseTaskGate(release)
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
		view, err := conversation.ContextView(bg, 0)
		if err != nil || view.Head == nil || view.Head.Kind != "pi.compaction" {
			t.Fatal("background summary not placed", view, err)
		}
		if summaries.Load() != 1 || requests.Load() != 1 {
			t.Fatal("background duplicate/automatic request", summaries.Load(), requests.Load())
		}
	})
}
func TestContextEstimateUsesLatestUsageOnlyWithinActiveHead(t *testing.T) {
	view := ContextView{Entries: []Entry{{ID: 2}, {ID: 3}, {ID: 4}}, Contributions: [][]MessageReceipt{{userReceipt(strings.Repeat("old", 100))}, {{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "answer"}}, Usage: &goai.Usage{Input: 50, Output: 10, CacheRead: 20, CacheWrite: 5}}}, {userReceipt("recent")}}}
	for _, contribution := range view.Contributions {
		view.Messages = append(view.Messages, contribution...)
	}
	want := 85 + goai.EstimateMessageTokens(receiptMessage(userReceipt("recent")))
	if got := estimateContextTokens(view); got != want {
		t.Fatal(got, want)
	}
	view.Contributions[1][0].Usage.TotalTokens = 120
	if got := estimateContextTokens(view); got != 120+goai.EstimateMessageTokens(receiptMessage(userReceipt("recent"))) {
		t.Fatal("reported total not used", got)
	}
	view.Head = &Entry{ID: 4, Head: 4}
	view.Entries = view.Entries[2:]
	view.Contributions = view.Contributions[2:]
	view.Messages = view.Contributions[0]
	if got := estimateContextTokens(view); got != goai.EstimateMessageTokens(receiptMessage(userReceipt("recent"))) {
		t.Fatal("usage crossed head", got)
	}
}
