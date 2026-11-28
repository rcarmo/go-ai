package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.7.0 packages/durable/test/tool-search.test.ts:197–225.
// The public Harness path must search referenced parameter text, not dormant
// definitions. Receipt wording remains the established native M1c contract.
func TestToolSearchReferencesOnlyReachableSchema(t *testing.T) {
	registry := NewRegistry()
	parameters := json.RawMessage(`{"type":"object","properties":{"options":{"$ref":"#/$defs/options"}},"$defs":{"options":{"type":"object","properties":{"mode":{"type":"string","description":"reconciliation"}}},"unused":{"description":"forecast"}}}`)
	if err := registry.Register(ToolRegistration{
		Definition:     goai.Tool{Name: "ledger_inspect", Description: "Inspect records", Parameters: parameters},
		Implementation: "source-v170.ledger-inspect",
		Version:        1,
		ReplaySafe:     true,
		Deferred:       true,
		Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
			return ToolResult{Content: "inspected"}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	search, err := NewToolSearchExtension(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Install(&search); err != nil {
		t.Fatal(err)
	}

	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		events := make(chan goai.Event, 1)
		switch requests.Add(1) {
		case 1:
			events <- toolAnswer("forecast-call", ToolSearchName, JSON{"query": "forecast"})
		case 2:
			events <- toolAnswer("reconcile-call", ToolSearchName, JSON{"query": "reconciliation", "limit": 1})
		default:
			events <- terminal("done")
		}
		close(events)
		return events
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(bg, Input{Content: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, submission); result.Submission.Status != "done" {
		t.Fatalf("submission = %+v", result.Submission)
	}
	if requests.Load() != 3 {
		t.Fatalf("provider requests = %d; want 3", requests.Load())
	}
	first := v160ToolResultText(v160ToolResult(t, bg, conversation, "forecast-call"))
	if first != "No matching deferred tools found." {
		t.Fatalf("unreferenced definition matched: %q", first)
	}
	second := v160ToolResultText(v160ToolResult(t, bg, conversation, "reconcile-call"))
	if second != "Loaded tools: ledger_inspect" {
		t.Fatalf("referenced definition did not match: %q", second)
	}
	agent, err := conversation.Agent(bg)
	if err != nil {
		t.Fatal(err)
	}
	if toolNames120(agent.Tools) != "tool_search,ledger_inspect" || toolNames120(agent.Deferred) != "" {
		t.Fatalf("tools=%q deferred=%q", toolNames120(agent.Tools), toolNames120(agent.Deferred))
	}
}

// Source: pi v1.7.0 tool-search.test.ts:64–99. A per-call limit controls the
// public result and leaves other deferred declarations unloaded.
func TestToolSearchHonorsCallLimit(t *testing.T) {
	registry := NewRegistry()
	for _, name := range []string{"github_get_issue", "github_list_repos"} {
		if err := registry.Register(ToolRegistration{
			Definition:     goai.Tool{Name: name, Description: "GitHub utility", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)},
			Implementation: "source-v170." + name,
			Version:        1,
			ReplaySafe:     true,
			Deferred:       true,
			Execute: func(context.Context, any, *ToolAPI) (ToolResult, error) {
				return ToolResult{Content: "ok"}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	search, err := NewToolSearchExtension(0)
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Install(&search); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		events := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			events <- toolAnswer("limit-call", ToolSearchName, JSON{"query": "GitHub", "limit": 1})
		} else {
			events <- terminal("done")
		}
		close(events)
		return events
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(bg, Input{Content: "find"})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, submission); result.Submission.Status != "done" {
		t.Fatalf("submission = %+v", result.Submission)
	}
	agent, err := conversation.Agent(bg)
	if err != nil {
		t.Fatal(err)
	}
	if toolNames120(agent.Tools) != "tool_search,github_get_issue" || toolNames120(agent.Deferred) != "github_list_repos" {
		t.Fatalf("tools=%q deferred=%q", toolNames120(agent.Tools), toolNames120(agent.Deferred))
	}
	result := v160ToolResult(t, bg, conversation, "limit-call")
	loaded, ok := result.Details["loaded"].([]any)
	if !ok || len(loaded) != 1 {
		t.Fatalf("loaded details = %#v", result.Details)
	}
}
