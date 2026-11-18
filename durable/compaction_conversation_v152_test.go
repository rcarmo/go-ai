package durable

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV152ConversationCompactionPrecedence(t *testing.T) {
	for _, tc := range []struct {
		host, local, want int
		mismatched        bool
	}{{0, 0, 400, false}, {250, 0, 200, false}, {250, 125, 100, false}, {0, 125, 400, true}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			caps := make(chan int, 1)
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
				cap := 0
				if opts != nil && opts.MaxTokens != nil {
					cap = *opts.MaxTokens
				}
				caps <- cap
				ch := make(chan goai.Event, 1)
				ch <- terminal("summary")
				close(ch)
				return ch
			})
			models := options.Models
			options.Models = func(p goai.Provider, id string) *goai.Model {
				model := *models(p, id)
				model.MaxTokens = 900
				return &model
			}
			key := string(ref.Provider) + "/" + ref.ID
			host := &CompactionSettings{ReserveTokens: compactionTestInt(1000), KeepRecentTokens: compactionTestInt(0)}
			if tc.host != 0 {
				host.ModelOverrides = map[string]CompactionSettings{key: {ReserveTokens: compactionTestInt(tc.host)}}
			}
			options.Settings = &HarnessSettings{Compaction: host}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			_, err = c.Commit(bg, func(tx *Tx) (any, error) {
				for _, text := range []string{"old history", "recent history"} {
					if _, err := tx.AppendEntry(c.ID(), EntryDraft{"kind": "pi.user", "model": []any{JSON{"role": "user", "content": text, "timestamp": 1}}}); err != nil {
						return nil, err
					}
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			local := &CompactionSettings{ReserveTokens: compactionTestInt(500)}
			if tc.local != 0 {
				localKey := key
				if tc.mismatched {
					localKey += "-other"
				}
				local.ModelOverrides = map[string]CompactionSettings{localKey: {ReserveTokens: compactionTestInt(tc.local)}}
			}
			if err := c.Configure(bg, AgentChange{Compaction: local}); err != nil {
				t.Fatal(err)
			}
			agent, err := c.Agent(bg)
			if err != nil || agent.Configuration.Model != ref || !reflect.DeepEqual(agent.Configuration.Compaction, local) {
				t.Fatal(agent.Configuration, err)
			}
			id, err := c.Compact(bg, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := h.WaitForTask(bg, id)
			if err != nil || result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
				t.Fatal(result, err)
			}
			select {
			case got := <-caps:
				if got != tc.want {
					t.Fatalf("summary maxTokens = %d, want %d", got, tc.want)
				}
			default:
				t.Fatal("no summary request")
			}
		})
	}
}

func TestSourceV152ConversationCompactionSettingsReplaceAndClear(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, nil)
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	key := string(ref.Provider) + "/" + ref.ID
	local := &CompactionSettings{KeepRecentTokens: compactionTestInt(0), ModelOverrides: map[string]CompactionSettings{key: {ReserveTokens: compactionTestInt(10)}}}
	if err := c.Configure(bg, AgentChange{Compaction: local}); err != nil {
		t.Fatal(err)
	}
	assertPolicy := func(expected *CompactionSettings) {
		t.Helper()
		agent, err := c.Agent(bg)
		if err != nil || !reflect.DeepEqual(agent.Configuration.Compaction, expected) || agent.Configuration.Model != ref {
			t.Fatal(agent.Configuration, err)
		}
	}
	assertPolicy(local)
	for _, bad := range []*CompactionSettings{{ReserveTokens: compactionTestInt(-1)}, {ModelOverrides: map[string]CompactionSettings{key: {BackgroundTokens: compactionTestInt(-1)}}}} {
		if err := c.Configure(bg, AgentChange{Compaction: bad}); err == nil {
			t.Fatal("invalid settings accepted")
		}
		assertPolicy(local)
	}
	if err := c.Configure(bg, AgentChange{CompactionSet: true}); err != nil {
		t.Fatal(err)
	}
	assertPolicy(nil)
}

func TestSourceV152ConversationCompactionEnableAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		disabled, modelRestores, overflow bool
		summaries                         int64
		status                            string
	}{
		{"inherited", false, false, false, 1, "done"},
		{"disabled", true, false, false, 0, "done"},
		{"model restores", true, true, false, 1, "done"},
		{"disabled on overflow", true, false, true, 0, "unanswered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var summaries atomic.Int64
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if strings.Contains(input.SystemPrompt, "context summarization assistant") {
					summaries.Add(1)
					ch <- terminal("summary")
				} else if tc.overflow {
					ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "maximum context length exceeded", Usage: &goai.Usage{Input: 1, TotalTokens: 1}}}
				} else {
					ch <- terminal("answer")
				}
				close(ch)
				return ch
			})
			if !tc.overflow {
				compactionTestModelWindow(&options, 1000)
			}
			enabled, disabled := true, false
			key := string(ref.Provider) + "/" + ref.ID
			options.Settings = &HarnessSettings{Compaction: &CompactionSettings{Enabled: &enabled, ReserveTokens: compactionTestInt(0), KeepRecentTokens: compactionTestInt(250), ModelOverrides: map[string]CompactionSettings{key: {Enabled: &enabled, ReserveTokens: compactionTestInt(300)}}}}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			_, err = c.Commit(bg, func(tx *Tx) (any, error) {
				for _, label := range []string{"u1", "u2", "u3"} {
					if _, err := tx.AppendEntry(c.ID(), EntryDraft{"kind": "pi.user", "model": []any{JSON{"role": "user", "content": label + strings.Repeat("x", 1000), "timestamp": 1}}}); err != nil {
						return nil, err
					}
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			var local *CompactionSettings
			if tc.disabled {
				local = &CompactionSettings{Enabled: &disabled}
			}
			if tc.modelRestores {
				local.ModelOverrides = map[string]CompactionSettings{key: {Enabled: &enabled}}
			}
			if err := c.Configure(bg, AgentChange{Compaction: local, CompactionSet: true}); err != nil {
				t.Fatal(err)
			}
			sub, err := c.Submit(bg, Input{Type: "input", Content: "u4" + strings.Repeat("x", 800)})
			if err != nil {
				t.Fatal(err)
			}
			settled := waitSubmission(t, sub)
			if settled.Record.Status != tc.status || summaries.Load() != tc.summaries {
				t.Fatal(settled.Record.Status, summaries.Load(), tc.status, tc.summaries)
			}
		})
	}
}
