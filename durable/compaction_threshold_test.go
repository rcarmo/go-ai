package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAutomaticCompactionStrictBlockingAndBackgroundThresholds(t *testing.T) {
	for _, background := range []bool{false, true} {
		for _, above := range []bool{false, true} {
			backends(t, func(t *testing.T, b backend) {
				var summaries atomic.Int64
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if strings.Contains(input.SystemPrompt, "context summarization assistant") {
						summaries.Add(1)
						ch <- terminal("summary")
					} else {
						ch <- terminal("answer")
					}
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
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				view, err := deriveContextView(state, conversation.ID(), 0, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				tokens := estimateContextTokens(view) + goai.EstimateMessageTokens(receiptMessage(userReceipt("new")))
				policy := CompactionPolicy{Enabled: true, TriggerTokens: tokens, KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("recent")))}
				if above {
					policy.TriggerTokens--
				}
				if background {
					policy.TriggerTokens += 100
					policy.BackgroundTokens = 100
				}
				if err := conversation.ConfigurePatch(bg, AgentPatch{Settings: &RequestSettings{Compaction: policy}}); err != nil {
					t.Fatal(err)
				}
				sub, err := conversation.Submit(bg, Input{Content: "new"})
				if err != nil {
					t.Fatal(err)
				}
				waitSubmission(t, sub)
				state, err = h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				var compaction ID
				for _, task := range state.Tasks {
					if task.Kind == "task.pi.compaction" {
						compaction = task.ID
						if (task.Owner == 0) != background {
							t.Fatal("threshold ownership", task, background)
						}
					}
				}
				if !above {
					if compaction != 0 || summaries.Load() != 0 {
						t.Fatal("equality must not compact", compaction, summaries.Load())
					}
					return
				}
				if compaction == 0 {
					t.Fatal("above threshold did not compact", tokens, policy)
				}
				waitPublicTask(t, h, compaction)
				if summaries.Load() != 1 {
					t.Fatal("summary count", summaries.Load())
				}
			})
		}
	}
}
