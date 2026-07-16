package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestXAIResponsesGrok45UsesResponsesWithCompatibleFields(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderXAI, "grok-4.5")
	if model == nil {
		t.Fatal("missing xAI grok-4.5")
	}
	if model.Api != goai.ApiOpenAIResponses {
		t.Fatalf("api=%q, want openai-responses", model.Api)
	}
	levels := goai.GetSupportedThinkingLevels(model)
	if !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingLow)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingMedium)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingHigh)) || hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingMinimal)) {
		t.Fatalf("unexpected thinking levels: %#v", levels)
	}

	reasoning := goai.ThinkingMedium
	sessionID := "pi-session-123"
	req := buildRequest(model, &goai.Context{SystemPrompt: "You are a careful coding assistant.", Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "xai-test-token", SessionID: sessionID, CacheRetention: goai.CacheRetentionLong, Reasoning: &reasoning})
	if req.Model != "grok-4.5" || !req.Stream || req.Store || req.PromptCacheKey != sessionID {
		t.Fatalf("unexpected request basics: %#v", req)
	}
	if req.Reasoning == nil || req.Reasoning.Effort != "medium" {
		t.Fatalf("reasoning=%#v, want medium", req.Reasoning)
	}
	if len(req.Include) != 1 || req.Include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include=%#v", req.Include)
	}
	if req.PromptCacheRetention != "" {
		t.Fatalf("xAI responses should not send prompt_cache_retention, got %q", req.PromptCacheRetention)
	}
	body, err := json.Marshal(req.Input)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "developer") || !strings.Contains(string(body), "You are a careful coding assistant.") {
		t.Fatalf("input missing developer system prompt: %s", body)
	}
}

func hasThinkingLevel(levels []goai.ModelThinkingLevel, want goai.ModelThinkingLevel) bool {
	for _, level := range levels {
		if level == want {
			return true
		}
	}
	return false
}
