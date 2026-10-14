package durable

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func registerSearchFixture120(t *testing.T, r *Registry) {
	t.Helper()
	for _, item := range []struct {
		name, description string
		deferred          bool
		callers           []string
	}{{"alpha", "baseline", false, nil}, {"github_get_issue", "Fetch a GitHub issue", true, nil}, {"github_list_repos", "List GitHub repositories", true, nil}, {"hidden_internal", "GitHub issue internal", true, []string{"tools"}}, {"omega", "baseline", false, nil}} {
		if e := r.Register(ToolRegistration{Definition: goai.Tool{Name: item.name, Description: item.description, Parameters: json.RawMessage(`{"type":"object","properties":{"issueNumber":{"type":"integer","description":"Issue identifier"}}}`)}, Implementation: item.name, Version: 1, ReplaySafe: true, Deferred: item.deferred, Callers: item.callers, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "ok"}, nil }}); e != nil {
			t.Fatal(e)
		}
	}
	ext, e := NewToolSearchExtension(2)
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Install(&ext); e != nil {
		t.Fatal(e)
	}
}
func toolNames120(tools []goai.Tool) string {
	names := []string{}
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return strings.Join(names, ",")
}
func TestToolSearch120DeferredSelectionAndDetachedAgent(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		r := NewRegistry()
		registerSearchFixture120(t, r)
		h := openHarness(t, b.store, Options{Registry: r})
		snapshot, e := r.taskSnapshot(h.session.limits)
		if e != nil {
			t.Fatal(e)
		}
		agent, _, e := h.resolveTaskAgent(agentState{}, snapshot)
		if e != nil {
			t.Fatal(e)
		}
		if toolNames120(agent.Tools) != "alpha,omega,tool_search" || toolNames120(agent.Deferred) != "github_get_issue,github_list_repos" {
			t.Fatal(toolNames120(agent.Tools), toolNames120(agent.Deferred))
		}
		if strings.Contains(toolNames120(agent.Callable), ToolSearchName) || !strings.Contains(toolNames120(agent.Callable), "hidden_internal") {
			t.Fatal(toolNames120(agent.Callable))
		}
		copy, e := copyTaskAgent(agent, h.session.limits)
		if e != nil {
			t.Fatal(e)
		}
		copy.Deferred[0].Parameters[0] = 'x'
		if agent.Deferred[0].Parameters[0] != '{' {
			t.Fatal("deferred schema alias")
		}
		configured := agentState{ModelTools: &ExtensionFilter{Add: []string{"github_get_issue"}, Remove: []string{"alpha"}}}
		agent, _, e = h.resolveTaskAgent(configured, snapshot)
		if e != nil {
			t.Fatal(e)
		}
		if toolNames120(agent.Tools) != "omega,tool_search,github_get_issue" || toolNames120(agent.Deferred) != "github_list_repos" {
			t.Fatal(agent)
		}
		excluded := []string{"github_get_issue"}
		configured.ToolsRemoved = &excluded
		agent, _, e = h.resolveTaskAgent(configured, snapshot)
		if e != nil || strings.Contains(toolNames120(agent.Tools), "github_get_issue") || strings.Contains(toolNames120(agent.Deferred), "github_get_issue") {
			t.Fatal(agent, e)
		}
	})
}
func TestToolSearch120LoadsPersistsAndDoesNotExecuteDiscoveredTool(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var calls atomic.Int64
		failure := make(chan string, 8)
		var summaries atomic.Int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				Messages []struct {
					Content any `json:"content"`
				} `json:"messages"`
				Tools []struct {
					Function struct {
						Name string `json:"name"`
					} `json:"function"`
				} `json:"tools"`
			}
			if e := json.NewDecoder(req.Body).Decode(&body); e != nil {
				failure <- e.Error()
			}
			if text, _ := json.Marshal(body.Messages); strings.Contains(string(text), "Summarise") {
				summaries.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"search summary\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
				return
			}
			names := []string{}
			for _, tool := range body.Tools {
				names = append(names, tool.Function.Name)
			}
			joined := strings.Join(names, ",")
			if calls.Add(1) == 1 {
				if strings.Contains(joined, "github_") {
					failure <- "deferred initially offered"
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"id\":\"search\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"search-call\",\"type\":\"function\",\"function\":{\"name\":\"tool_search\",\"arguments\":\"{\\\"query\\\":\\\"GitHub issue\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\ndata: [DONE]\n\n"))
				return
			}
			if !strings.Contains(joined, "github_get_issue") || strings.Contains(joined, "hidden_internal") {
				failure <- "loaded tool missing or internal tool exposed: " + joined
			}
			taskHTTPRound(w, nil)
		}))
		defer server.Close()
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL
		model.Headers = map[string]string{"Authorization": "Bearer durable-test"}
		r := NewRegistry()
		registerSearchFixture120(t, r)
		h := openHarness(t, b.store, Options{Registry: r, Models: func(goai.Provider, string) *goai.Model { return model }})
		conv := root(t, h, ModelRef{model.Provider, model.ID})
		sub, e := conv.Submit(bg, Input{Content: "find GitHub issue"})
		if e != nil {
			t.Fatal(e)
		}
		if result := waitSubmission(t, sub); result.Submission.Status != "done" {
			t.Fatal(result)
		}
		state, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		select {
		case err := <-failure:
			for _, doc := range state.Documents {
				if doc.Kind == "pi.agent" {
					t.Logf("agent: %#v", doc.Value)
				}
			}
			t.Fatal(err)
		default:
		}
		doc, ok := agentDocument(state, conv.ID())
		if !ok {
			t.Fatal("agent missing")
		}
		var agent agentState
		if e = fromObject(doc.Value, &agent, h.session.limits); e != nil || agent.ModelTools == nil || len(agent.ModelTools.Add) == 0 || agent.ModelTools.Add[0] != "github_get_issue" {
			t.Fatal(agent, e)
		}
		var latest ID
		for id, entry := range state.Entries {
			if entry.Conversation == conv.ID() && id > latest {
				latest = id
			}
		}
		fork, e := conv.Fork(bg, latest)
		if e != nil {
			t.Fatal(e)
		}
		forkState, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		forkDoc, ok := agentDocument(forkState, fork.ID())
		if !ok {
			t.Fatal("fork lost branch-local model tools")
		}
		var forkAgent agentState
		if e = fromObject(forkDoc.Value, &forkAgent, h.session.limits); e != nil || forkAgent.ModelTools == nil || forkAgent.ModelTools.Add[0] != "github_get_issue" {
			t.Fatal(forkAgent, e)
		}
		compact, e := conv.Compact(bg, CompactionOptions{KeepRecentTokensOverride: func() *int { n := 0; return &n }()})
		if e != nil {
			t.Fatal(e)
		}
		if result, e := h.WaitForTask(bg, compact); e != nil || result.State.Outcome.Status != "completed" {
			t.Fatal(result, e)
		}
		if summaries.Load() != 1 {
			t.Fatal("compaction did not use summary request", summaries.Load())
		}
		follow, e := conv.Submit(bg, Input{Content: "after compaction"})
		if e != nil {
			t.Fatal(e)
		}
		if result := waitSubmission(t, follow); result.Submission.Status != "done" {
			t.Fatal(result)
		}
		select {
		case err := <-failure:
			t.Fatal(err)
		default:
		}
		// Verify settled controls survive reopen; no discovered tool was dispatched.
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				var cp toolCheckpoint
				if e = fromObject(task.Checkpoint, &cp, h.session.limits); e != nil {
					t.Fatal(e)
				}
				if cp.Offer.Name != ToolSearchName {
					t.Fatal("search executed discovered tool", cp.Offer.Name)
				}
			}
		}
		if e = h.Close(bg); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		opened, e := next.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		doc, ok = agentDocument(opened, conv.ID())
		if !ok {
			t.Fatal("agent disappeared")
		}
		if e = fromObject(doc.Value, &agent, next.session.limits); e != nil || agent.ModelTools == nil || agent.ModelTools.Add[0] != "github_get_issue" {
			t.Fatal(agent, e)
		}
	})
}
func TestToolSearch120SchemaTextAndInputValidation(t *testing.T) {
	text := schemaSearchText(map[string]any{"description": "top", "properties": map[string]any{"issueNumber": map[string]any{"description": "identifier"}}, "anyOf": []any{map[string]any{"description": "alternative"}}})
	if text != "top alternative issueNumber identifier" {
		t.Fatal(text)
	}
	if _, e := ToolSearchRegistration(-1); e == nil {
		t.Fatal("negative limit")
	}
	reg, e := ToolSearchRegistration(1)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = reg.Execute(bg, JSON{"query": "  "}, nil); e == nil {
		t.Fatal("blank query")
	}
}
