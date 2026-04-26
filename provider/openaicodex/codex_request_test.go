package openaicodex

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildCodexRequestMatchesPiaiShape(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses}
	sess := "sess-123"
	reasoning := goai.ThinkingLevel("minimal")
	conv := &goai.Context{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "hi"}}}},
		Tools:        []goai.Tool{{Name: "shell", Description: "run shell", Parameters: json.RawMessage(`{"type":"object"}`)}},
	}
	req := buildCodexRequest(model, conv, &goai.StreamOptions{SessionID: sess, Reasoning: &reasoning})
	if !req.Stream {
		t.Fatalf("expected stream=true")
	}
	if req.Store {
		t.Fatalf("expected store to be false")
	}
	if req.Instructions != conv.SystemPrompt {
		t.Fatalf("expected instructions %q, got %q", conv.SystemPrompt, req.Instructions)
	}
	if req.PromptCacheKey != sess {
		t.Fatalf("expected prompt cache key %q, got %q", sess, req.PromptCacheKey)
	}
	if req.ToolChoice != "auto" {
		t.Fatalf("expected tool_choice auto, got %q", req.ToolChoice)
	}
	if req.ParallelToolCalls == nil || !*req.ParallelToolCalls {
		t.Fatalf("expected parallel_tool_calls=true")
	}
	if len(req.Include) != 1 || req.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("unexpected include: %#v", req.Include)
	}
	rm, ok := req.Reasoning.(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning map, got %#v", req.Reasoning)
	}
	if rm["effort"] != "low" {
		t.Fatalf("expected clamped minimal->low, got %#v", rm["effort"])
	}
	var input []map[string]any
	if err := json.Unmarshal(req.Input, &input); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}
	if len(input) == 0 || input[0]["role"] != "user" {
		t.Fatalf("expected first input item to be user message, got %#v", input)
	}
}

func TestBuildCodexHeadersAddsAccountAndExperimentalHeaders(t *testing.T) {
	token := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	accountID, err := extractCodexAccountID(token)
	if err != nil {
		t.Fatalf("extractCodexAccountID: %v", err)
	}
	if accountID != "acct_123" {
		t.Fatalf("unexpected account id: %q", accountID)
	}
	h := buildCodexSSEHeaders(map[string]string{"x-model": "1"}, map[string]string{"x-opt": "2"}, accountID, token, "sess-1")
	if h.Get("chatgpt-account-id") != "acct_123" || h.Get("OpenAI-Beta") != "responses=experimental" || h.Get("session_id") != "sess-1" {
		t.Fatalf("unexpected SSE headers: %#v", h)
	}
	wh := buildCodexWebSocketHeaders(nil, nil, accountID, token, "req-1")
	if wh.Get("OpenAI-Beta") != "responses_websockets=2026-02-06" || wh.Get("x-client-request-id") != "req-1" {
		t.Fatalf("unexpected WS headers: %#v", wh)
	}
}
