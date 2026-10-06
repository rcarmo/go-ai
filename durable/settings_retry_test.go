package durable

import (
	"context"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestResolvedRetryDelayPreservesExplicitZeroAndReferenceBackoff(t *testing.T) {
	for _, tc := range []struct {
		base, cap int64
		attempt   int
		want      int64
	}{{2000, 60000, 1, 2000}, {2000, 60000, 6, 60000}, {0, 60000, 3, 0}, {2000, 0, 3, 0}, {1000, 1500, 2, 1500}} {
		if got := retryDelay(RetryPolicy{BaseDelayMs: tc.base, MaxDelayMs: tc.cap}, tc.attempt); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

func TestCompactionResolvedReserveBudgetModelCapAndStreamSettings(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		reserve, modelMax, want int
	}{{"reserve", 1000, 4096, 800}, {"model-cap", 1000, 256, 256}, {"zero", 0, 4096, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, release := make(chan struct{}), make(chan struct{})
				defer func() {
					select {
					case <-release:
					default:
						close(release)
					}
				}()
				ref, options := setupFakeAPI(t, func(_ context.Context, model *goai.Model, input *goai.Context, stream *goai.StreamOptions) <-chan goai.Event {
					if stream.MaxTokens == nil || *stream.MaxTokens != tc.want || stream.CacheRetention != goai.CacheRetentionNone || (stream.Reasoning == nil || *stream.Reasoning != "low") || stream.Deferred != nil {
						t.Errorf("summary stream settings %+v", stream)
					}
					ch := make(chan goai.Event, 1)
					close(entered)
					go func() { <-release; ch <- terminal("summary"); close(ch) }()
					return ch
				})
				models := options.Models
				options.Models = func(provider goai.Provider, id string) *goai.Model {
					model := models(provider, id)
					if model != nil {
						copy := *model
						copy.MaxTokens = tc.modelMax
						return &copy
					}
					return nil
				}
				options.Settings = &HarnessSettings{Compaction: &CompactionSettings{ReserveTokens: &tc.reserve}}
				h := openHarness(t, b.store, options)
				conversation, err := h.CreateConversation(bg, AgentChange{Model: ref, ThinkingLevel: "low"})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.session.Commit(bg, func(tx *Tx) error {
					for _, text := range []string{"old", "recent"} {
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						receipt, err := dtoObject(MessageReceipt{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: text}}}, tx.limits)
						if err != nil {
							return err
						}
						if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: receipt}); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokens: goai.EstimateMessageTokens(goai.Message{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "recent"}}})})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					state, _ := h.Snapshot(bg)
					record, _ := CanonicalTask(state.Tasks[id], h.session.limits)
					t.Fatalf("summary did not start: %+v %+v", state.Tasks[id], record.State.Outcome.Error)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				task, err := CanonicalTask(state.Tasks[id], h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				var policy RetryPolicy
				if err := fromObject(JSON(task.State.Checkpoint["retry"].(map[string]any)), &policy, h.session.limits); err != nil || policy != defaultHarnessSettings().Retry {
					t.Fatal("summary retry defaults", policy, err)
				}
				close(release)
				if record := waitPublicTask(t, h, id); record.State.Outcome.Status != "completed" {
					t.Fatal(record)
				}
			})
		})
	}
}
