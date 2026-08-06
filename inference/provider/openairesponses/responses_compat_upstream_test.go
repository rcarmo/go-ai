package openairesponses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type capturedResponsesRequest struct {
	Header http.Header
	Body   map[string]any
	Path   string
}

func captureResponsesRequest(t *testing.T, model *goai.Model, opts *goai.StreamOptions) capturedResponsesRequest {
	t.Helper()
	var captured capturedResponsesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		captured.Header = r.Header.Clone()
		captured.Path = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(body, &captured.Body); err != nil {
			t.Fatalf("unmarshal request body: %v; body=%s", err, body)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n"))
	}))
	t.Cleanup(server.Close)

	requestModel := *model
	if strings.Contains(requestModel.BaseURL, "openrouter.ai") {
		requestModel.BaseURL = server.URL + "/openrouter.ai"
	} else {
		requestModel.BaseURL = server.URL
	}
	if opts == nil {
		opts = &goai.StreamOptions{}
	}
	if opts.APIKey == "" {
		opts.APIKey = "test-key"
	}
	for range streamResponses(context.Background(), &requestModel, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
	}
	return captured
}

func TestOpenAIResponsesCompatSessionAffinityFormats(t *testing.T) {
	for _, tc := range []struct {
		name        string
		model       *goai.Model
		wantSession string
		wantClient  string
		wantX       string
	}{
		{
			name:        "openai default",
			model:       &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses},
			wantSession: "session-123",
			wantClient:  "session-123",
		},
		{
			name:  "openrouter explicit compat",
			model: &goai.Model{ID: "gpt-test", Provider: "proxy", Api: goai.ApiOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openrouter"}},
			wantX: "session-123",
		},
		{
			name:  "openrouter provider autodetect",
			model: &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAIResponses},
			wantX: "session-123",
		},
		{
			name:  "openrouter endpoint autodetect",
			model: &goai.Model{ID: "gpt-test", Provider: "proxy", Api: goai.ApiOpenAIResponses, BaseURL: "https://openrouter.ai/api/v1"},
			wantX: "session-123",
		},
		{
			name:       "openai no session explicit compat",
			model:      &goai.Model{ID: "gpt-test", Provider: "proxy", Api: goai.ApiOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openai-nosession"}},
			wantClient: "session-123",
		},
		{
			name:       "opencode generated compat",
			model:      &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenCode, Api: goai.ApiOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SessionAffinityFormat: "openai-nosession"}},
			wantClient: "session-123",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			captured := captureResponsesRequest(t, tc.model, &goai.StreamOptions{SessionID: "session-123"})
			if got := captured.Header.Get("session_id"); got != tc.wantSession {
				t.Fatalf("session_id=%q, want %q; headers=%#v", got, tc.wantSession, captured.Header)
			}
			if got := captured.Header.Get("x-client-request-id"); got != tc.wantClient {
				t.Fatalf("x-client-request-id=%q, want %q; headers=%#v", got, tc.wantClient, captured.Header)
			}
			if got := captured.Header.Get("x-session-id"); got != tc.wantX {
				t.Fatalf("x-session-id=%q, want %q; headers=%#v", got, tc.wantX, captured.Header)
			}
			if got := captured.Body["prompt_cache_key"]; got != "session-123" {
				t.Fatalf("prompt_cache_key=%#v, want session-123", got)
			}
		})
	}
}

func TestOpenAIResponsesCacheRetentionNoneSuppressesGeneratedAffinityHeaders(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	captured := captureResponsesRequest(t, model, &goai.StreamOptions{SessionID: "session-123", CacheRetention: goai.CacheRetentionNone})
	if captured.Header.Get("session_id") != "" || captured.Header.Get("x-client-request-id") != "" || captured.Header.Get("x-session-id") != "" {
		t.Fatalf("generated affinity headers should be omitted when cacheRetention none: %#v", captured.Header)
	}
	if _, ok := captured.Body["prompt_cache_key"]; ok {
		t.Fatalf("prompt_cache_key should be omitted when cacheRetention none: %#v", captured.Body)
	}
}

func TestOpenAIResponsesExplicitHeadersOverrideGeneratedAffinityHeaders(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	captured := captureResponsesRequest(t, model, &goai.StreamOptions{
		SessionID: "session-123",
		Headers: map[string]string{
			"session_id":          "override-session",
			"x-client-request-id": "override-request",
		},
	})
	if got := captured.Header.Get("session_id"); got != "override-session" {
		t.Fatalf("session_id=%q, want override-session", got)
	}
	if got := captured.Header.Get("x-client-request-id"); got != "override-request" {
		t.Fatalf("x-client-request-id=%q, want override-request", got)
	}
}

func TestOpenAIResponsesRequiredToolChoiceSerializes(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{{Name: "ping", Description: "Ping", Parameters: json.RawMessage(`{"type":"object"}`)}}}, &goai.StreamOptions{ToolChoice: goai.ToolChoiceRequired})
	if req.ToolChoice != "required" {
		t.Fatalf("tool_choice=%q, want required", req.ToolChoice)
	}
}

func TestOpenAIResponsesServiceTierCostMultipliers(t *testing.T) {
	for _, tc := range []struct {
		modelID     string
		serviceTier string
		multiplier  float64
	}{
		{"gpt-5.4", "priority", 2},
		{"gpt-5.5", "priority", 2.5},
		{"gpt-5.5", "flex", 0.5},
	} {
		t.Run(tc.modelID+"/"+tc.serviceTier, func(t *testing.T) {
			model := &goai.Model{ID: tc.modelID, Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, Cost: goai.ModelCost{Input: 2.5, Output: 15, CacheRead: 0.25, CacheWrite: 3}}
			body := strings.NewReader(`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","service_tier":"` + tc.serviceTier + `","usage":{"input_tokens":400000,"output_tokens":100000,"total_tokens":500000,"input_tokens_details":{"cached_tokens":100000,"cache_write_tokens":100000}}}}` + "\n\n")
			ch := make(chan goai.Event, 8)
			processStreamWithOptions(body, model, nil, ch)
			close(ch)
			var done *goai.DoneEvent
			for ev := range ch {
				if d, ok := ev.(*goai.DoneEvent); ok {
					done = d
				}
			}
			if done == nil || done.Message == nil || done.Message.Usage == nil {
				t.Fatalf("missing done usage: %#v", done)
			}
			wantInput := 200000.0 * model.Cost.Input / 1_000_000.0 * tc.multiplier
			wantOutput := 100000.0 * model.Cost.Output / 1_000_000.0 * tc.multiplier
			wantCacheRead := 100000.0 * model.Cost.CacheRead / 1_000_000.0 * tc.multiplier
			wantCacheWrite := 100000.0 * model.Cost.CacheWrite / 1_000_000.0 * tc.multiplier
			cost := done.Message.Usage.Cost
			if cost.Input != wantInput || cost.Output != wantOutput || cost.CacheRead != wantCacheRead || cost.CacheWrite != wantCacheWrite {
				t.Fatalf("cost=%#v, want input=%v output=%v cacheRead=%v cacheWrite=%v", cost, wantInput, wantOutput, wantCacheRead, wantCacheWrite)
			}
			if cost.Total != cost.Input+cost.Output+cost.CacheRead+cost.CacheWrite {
				t.Fatalf("total=%v does not match components %#v", cost.Total, cost)
			}
		})
	}
}

func TestOpenAIResponsesOffReasoningNoneMatrix(t *testing.T) {
	offNone := "none"
	for _, tc := range []struct {
		name          string
		model         *goai.Model
		wantReasoning bool
		wantEffort    string
	}{
		{name: "off mapped emits none", model: &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: &offNone}}, wantReasoning: true, wantEffort: "none"},
		{name: "off unsupported omits reasoning", model: &goai.Model{ID: "gpt-5", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil}}},
		{name: "copilot default omits reasoning", model: &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderGitHubCopilot, Api: goai.ApiOpenAIResponses, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: &offNone}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := buildRequest(tc.model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, nil)
			if !tc.wantReasoning {
				if req.Reasoning != nil {
					t.Fatalf("reasoning=%#v, want omitted", req.Reasoning)
				}
				return
			}
			if req.Reasoning == nil || req.Reasoning.Effort != tc.wantEffort {
				t.Fatalf("reasoning=%#v, want effort %q", req.Reasoning, tc.wantEffort)
			}
		})
	}
}

func TestXAIResponsesAlwaysRequestsEncryptedReasoningInclude(t *testing.T) {
	model := &goai.Model{ID: "grok-4.5", Provider: goai.ProviderXAI, Api: goai.ApiOpenAIResponses, Reasoning: true, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsLongCacheRetention: boolPtr(false)}}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, nil)
	if len(req.Include) != 1 || req.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include=%#v, want encrypted reasoning include", req.Include)
	}
}

func boolPtr(v bool) *bool { return &v }
