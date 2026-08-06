package openairesponses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestXAIUpstreamV0811Grok45UsesResponses(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderXAI, "grok-4.5")
	if model == nil {
		t.Fatal("missing xAI grok-4.5")
	}
	if model.Api != goai.ApiOpenAIResponses {
		t.Fatalf("api=%q, want openai-responses per exact upstream v0.81.1 tag 20be4b18", model.Api)
	}
	if model.ContextWindow != 500000 || model.MaxTokens != 500000 || model.Cost.Input != 2 || model.Cost.Output != 6 || model.Cost.CacheRead != 0.3 {
		t.Fatalf("unexpected xAI grok-4.5 v0.81.1 metadata: %#v", model)
	}
	levels := goai.GetSupportedThinkingLevels(model)
	wantLevels := []goai.ModelThinkingLevel{goai.ModelThinkingLevel(goai.ThinkingLow), goai.ModelThinkingLevel(goai.ThinkingMedium), goai.ModelThinkingLevel(goai.ThinkingHigh)}
	if len(levels) != len(wantLevels) {
		t.Fatalf("thinking levels=%#v, want exact %#v", levels, wantLevels)
	}
	for i, want := range wantLevels {
		if levels[i] != want {
			t.Fatalf("thinking levels=%#v, want exact %#v", levels, wantLevels)
		}
	}
	if model.ResponsesCompat == nil || model.ResponsesCompat.SupportsLongCacheRetention == nil || *model.ResponsesCompat.SupportsLongCacheRetention {
		t.Fatalf("expected xAI responses long cache retention disabled, got %#v", model.ResponsesCompat)
	}
}

func TestXAIResponsesRequestShapeMatchesUpstream(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderXAI, "grok-4.5")
	if model == nil {
		t.Fatal("missing xAI grok-4.5")
	}
	if got, want := model.BaseURL, "https://api.x.ai/v1"; got != want {
		t.Fatalf("xAI baseURL=%q, want %q", got, want)
	}

	var gotPath string
	var gotAuth string
	var gotSessionID string
	var gotClientRequestID string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotSessionID = r.Header.Get("session_id")
		gotClientRequestID = r.Header.Get("x-client-request-id")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("unmarshal xAI request: %v; body=%s", err, raw)
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_xai_test\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":0}}}}\n\n"))
	}))
	defer server.Close()

	requestModel := *model
	requestModel.BaseURL = server.URL
	reasoning := goai.ThinkingMedium
	for range streamResponses(context.Background(), &requestModel, &goai.Context{
		SystemPrompt: "You are a careful coding assistant.",
		Messages:     []goai.Message{goai.UserMessage("hello")},
	}, &goai.StreamOptions{APIKey: "xai-test-token", SessionID: "pi-session-123", CacheRetention: goai.CacheRetentionLong, Reasoning: &reasoning}) {
	}

	if gotPath != "/responses" {
		t.Fatalf("path=%q, want /responses", gotPath)
	}
	if gotAuth != "Bearer xai-test-token" {
		t.Fatalf("Authorization=%q, want bearer token", gotAuth)
	}
	if gotSessionID != "pi-session-123" || gotClientRequestID != "pi-session-123" {
		t.Fatalf("session headers session_id=%q x-client-request-id=%q", gotSessionID, gotClientRequestID)
	}
	if body["model"] != "grok-4.5" || body["store"] != false || body["stream"] != true || body["prompt_cache_key"] != "pi-session-123" {
		t.Fatalf("unexpected xAI request body core fields: %#v", body)
	}
	if _, ok := body["prompt_cache_retention"]; ok {
		t.Fatalf("prompt_cache_retention should be omitted for xAI: %#v", body)
	}
	reasoningBody, ok := body["reasoning"].(map[string]any)
	if !ok || reasoningBody["effort"] != "medium" {
		t.Fatalf("reasoning=%#v, want effort medium", body["reasoning"])
	}
	include, ok := body["include"].([]any)
	if !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include=%#v, want encrypted reasoning include", body["include"])
	}
	input, ok := body["input"].([]any)
	if !ok || len(input) == 0 {
		t.Fatalf("input=%#v, want non-empty input array", body["input"])
	}
	developer, ok := input[0].(map[string]any)
	if !ok || developer["role"] != "developer" || developer["content"] != "You are a careful coding assistant." {
		t.Fatalf("developer input=%#v", input[0])
	}
}
