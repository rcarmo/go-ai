package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAutomaticCompactionStrictBlockingAndBackgroundThresholds(t *testing.T) {
	for _, background := range []bool{false, true} {
		for _, above := range []bool{false, true} {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
				var window atomic.Int64
				window.Store(128000)
				resolve := options.Models
				options.Models = func(provider goai.Provider, id string) *goai.Model {
					model := *resolve(provider, id)
					model.ContextWindow = int(window.Load())
					return &model
				}
				h := openHarness(t, b.store, options)
				conversation := root(t, h, ref)
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
						if err := tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
							return nil, err
						}
					}
					return nil, nil
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
				policy := CompactionPolicy{Enabled: true, ReserveTokens: 1000, KeepRecentTokens: goai.EstimateMessageTokens(receiptMessage(userReceipt("recent")))}
				trigger := tokens
				if above {
					trigger--
				}
				if background {
					trigger += 100
					policy.BackgroundTokens = 100
				}
				window.Store(int64(trigger + policy.ReserveTokens))
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
					if task.Kind == "pi.compaction" {
						record, err := CanonicalTask(task, h.session.limits)
						if err != nil || !equalJSONValue(record.Input.Value, JSON{"reason": "threshold"}) {
							t.Fatal("automatic compaction leaked native input overrides", record, err)
						}
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
				record := waitPublicTask(t, h, compaction)
				key := "entryId"
				if background {
					key = "submissionId"
				}
				if record.State.Outcome.Result == nil {
					t.Fatal("missing present compaction result", record)
				}
				result := record.State.Outcome.Result.Value.(map[string]any)
				if len(result) != 1 || submissionEntryID(result[key]) == 0 {
					t.Error("source result must contain only "+key, result)
				}
				if summaries.Load() != 1 {
					t.Fatal("summary count", summaries.Load())
				}
			})
		}
	}
}

// Source harness/types.ts:566-583 at f9a2636 defines exactly these policy
// fields; model selection and reserveTokens determine threshold and budget.
// This is a source-derived public DTO audit, not an original test-body port.
func TestSourceCompactionPolicyFields(t *testing.T) {
	encoded, err := json.Marshal(CompactionPolicy{})
	if err != nil || !reflect.DeepEqual(dropinJSON(t, encoded), dropinJSON(t, []byte(`{"enabled":false,"reserveTokens":0,"keepRecentTokens":0,"backgroundTokens":0}`))) {
		t.Fatalf("required resolved policy fields omitted: %s, %v", encoded, err)
	}
	for _, test := range []struct {
		value  any
		fields []string
	}{
		{CompactionPolicy{}, []string{"enabled", "reserveTokens", "backgroundTokens", "keepRecentTokens"}},
		{CompactionSettings{}, []string{"modelOverrides", "enabled", "reserveTokens", "keepRecentTokens", "backgroundTokens"}},
	} {
		typ := reflect.TypeOf(test.value)
		var names []string
		for i := 0; i < typ.NumField(); i++ {
			name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			names = append(names, name)
		}
		if !reflect.DeepEqual(names, test.fields) {
			t.Fatalf("%s fields %v; original %v", typ, names, test.fields)
		}
	}
}

// Upstream open({contextWindow}) is expressed through the real model resolver.
func compactionTestModelWindow(options *Options, window int) {
	resolve := options.Models
	options.Models = func(provider goai.Provider, id string) *goai.Model {
		model := resolve(provider, id)
		if model == nil {
			return nil
		}
		owned := *model
		owned.ContextWindow = window
		return &owned
	}
}
