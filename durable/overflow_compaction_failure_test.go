package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOverflowFailedOrDeclinedCompactionEndsWithoutAnotherRequestOrReceipt(t *testing.T) {
	for _, decline := range []bool{false, true} {
		sqliteStore(t, func(t *testing.T, b sqliteFixture) {
			var requests, summaries atomic.Int64
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				text := "maximum context length exceeded"
				if strings.Contains(input.SystemPrompt, "context summarization assistant") {
					summaries.Add(1)
					text = "billing limit reached"
				} else {
					requests.Add(1)
				}
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: text, Content: []goai.ContentBlock{}}}
				close(ch)
				return ch
			})
			if decline {
				registry := NewRegistry()
				if err := registry.Install(&Extension{Name: "decline", CompactionHooks: CompactionHooks{BeforeCompact: func(context.Context, BeforeCompactInput, *HookAPI) (*CompactionDecision, error) {
					return &CompactionDecision{Decline: true}, nil
				}}}); err != nil {
					t.Fatal(err)
				}
				options.Registry = registry
			}
			h := openHarness(t, b.store, options)
			conversation := root(t, h, ref)
			if err := conversation.ConfigurePatch(bg, AgentPatch{Settings: &RequestSettings{Compaction: CompactionPolicy{Enabled: true, ReserveTokens: 28000, KeepRecentTokens: 1}}}); err != nil {
				t.Fatal(err)
			}
			_, err := h.session.Commit(bg, func(tx *Tx) (any, error) {
				for _, text := range []string{"old", "recent"} {
					id, err := tx.MintID()
					if err != nil {
						return nil, err
					}
					value, err := dtoObject(userReceipt(text), tx.limits)
					if err != nil {
						return nil, err
					}
					if err := tx.appendEntryRecord(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
						return nil, err
					}
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			sub, err := conversation.Submit(bg, Input{Content: "new"})
			if err != nil {
				t.Fatal(err)
			}
			result := waitSubmission(t, sub)
			if result.Submission.Status != "failed" || result.Message != nil || requests.Load() != 1 {
				t.Fatal(result, requests.Load())
			}
			record, err := CanonicalTask(result.Task, h.session.limits)
			if err != nil || record.State.Outcome.Error.Message != "maximum context length exceeded" {
				t.Fatal(record, err)
			}
			want := int64(1)
			if decline {
				want = 0
			}
			if summaries.Load() != want {
				t.Fatal("summary count", summaries.Load())
			}
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			assistants := 0
			for _, entry := range state.Entries {
				if legacyEntryValue(entry)["role"] == string(goai.RoleAssistant) {
					assistants++
				}
			}
			if assistants != 1 {
				t.Fatal("overflow receipt missing/duplicated", assistants)
			}
		})
	}
}
