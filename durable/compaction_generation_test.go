package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAutomaticOwnedCompactionRetriesThenGenerationUsesSummary(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var summaryCalls, answerCalls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if strings.Contains(input.SystemPrompt, "Summarise") {
				if summaryCalls.Add(1) == 1 {
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, StopReason: goai.StopReasonError, ErrorMessage: "overloaded_error", Usage: &goai.Usage{TotalTokens: 1}}}
				} else {
					ch <- terminal("earlier work summarised")
				}
			} else {
				answerCalls.Add(1)
				text := []string{}
				for _, message := range input.Messages {
					for _, block := range message.Content {
						text = append(text, block.Text)
					}
				}
				combined := strings.Join(text, "|")
				if !strings.Contains(combined, "earlier work summarised") || !strings.Contains(combined, "new question") || strings.Contains(combined, "very old") {
					t.Error("generation bypassed summary", combined)
				}
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Retry: RetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMs: 1}, Compaction: CompactionPolicy{Enabled: true, TriggerTokens: 1, KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("recent"))), MaxTokens: 50}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = conversation.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"very old question", "very old answer", "recent"} {
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
		result := waitSubmission(t, sub)
		if result.Submission.Status != "done" || summaryCalls.Load() != 2 || answerCalls.Load() != 1 {
			t.Fatal(result, summaryCalls.Load(), answerCalls.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, task := range state.Tasks {
			if task.Kind == "task.pi.compaction" {
				found = true
				if task.Owner != result.Task.ID || task.Status != "done" {
					t.Fatal("automatic compaction ownership", task)
				}
			}
		}
		if !found {
			t.Fatal("missing compaction task")
		}
	})
}

func TestOverflowCompactionOneRecoveryAndNoOrdinaryRetry(t *testing.T) {
	for _, persistent := range []bool{false, true} {
		t.Run(fmt.Sprint(persistent), func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var summaries, answers atomic.Int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if strings.Contains(input.SystemPrompt, "Summarise") {
						summaries.Add(1)
						ch <- terminal("overflow summary")
					} else {
						n := answers.Add(1)
						if n == 1 || persistent {
							ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, Content: []goai.ContentBlock{}, ErrorMessage: "maximum context length exceeded", Usage: &goai.Usage{Input: 1, TotalTokens: 1}}}
						} else {
							originals := 0
							summary := false
							for _, message := range input.Messages {
								for _, block := range message.Content {
									if block.Text == "new question" {
										originals++
									}
									if strings.Contains(block.Text, "overflow summary") {
										summary = true
									}
								}
							}
							if originals != 1 || !summary {
								t.Error("overflow continuation duplicated/dropped context", originals, summary, input.Messages)
							}
							ch <- terminal("recovered")
						}
					}
					close(ch)
					return ch
				})
				h := openHarness(t, b.store, options)
				conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Compaction: CompactionPolicy{Enabled: true, TriggerTokens: 100000, KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("new question")))}, Retry: RetryPolicy{Enabled: true, MaxRetries: 3, BaseDelayMs: 1}}})
				if err != nil {
					t.Fatal(err)
				}
				_, err = conversation.Commit(bg, func(tx *Tx) error {
					for _, text := range []string{"old question", "old answer", "recent"} {
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
				result := waitSubmission(t, sub)
				want := "done"
				if persistent {
					want = "failed"
				}
				if result.Submission.Status != want || summaries.Load() != 1 || answers.Load() != 2 {
					t.Fatal("overflow loop/retry", result, summaries.Load(), answers.Load())
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				compactions := 0
				for _, task := range state.Tasks {
					if task.Kind == "task.pi.compaction" {
						compactions++
						record, err := CanonicalTask(task, h.session.limits)
						if err != nil {
							t.Fatal(err)
						}
						if record.Input.Value.(map[string]any)["reason"] != "overflow" {
							t.Fatal(record)
						}
					}
				}
				if compactions != 1 {
					t.Fatal("overflow compaction duplicated", compactions)
				}
			})
		})
	}
}
