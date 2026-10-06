package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAutomaticCompactionRetryDecisionFollowsCurrentSettingsAfterResponse(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var summaries, answers atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if strings.Contains(input.SystemPrompt, "context summarization assistant") {
				summaries.Add(1)
				close(entered)
				go func() {
					<-release
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
					close(ch)
				}()
			} else {
				answers.Add(1)
				ch <- terminal("answer")
				close(ch)
			}
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Compaction: CompactionPolicy{Enabled: true, TriggerTokens: 1, KeepRecentTokens: 1, MaxTokens: 100}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.CommitTasks(bg, conversation.ID(), func(tx *Tx) error {
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
		sub, err := conversation.Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		disabled := false
		if err := h.SetSettings(bg, HarnessSettings{Retry: &RetrySettings{Enabled: &disabled}}); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		final := waitSubmission(t, sub)
		if final.Submission.Status != "done" || summaries.Load() != 1 || answers.Load() != 1 {
			t.Fatal("summary retried obsolete policy", final, summaries.Load(), answers.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == "task.pi.compaction" {
				record, err := CanonicalTask(task, h.session.limits)
				if err != nil || record.State.Outcome.Status != "failed" {
					t.Fatal(record, err)
				}
				if _, present := record.Input.Value.(map[string]any)["retry"]; present {
					t.Fatal("automatic retry policy pinned in input", record.Input)
				}
			}
		}
	})
}
