package google

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGoogleThinkingDisableGemini25UsesZeroBudget(t *testing.T) {
	req := buildRequest(&goai.Model{ID: "gemini-2.5-flash", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI, Reasoning: true}, googleThinkingDisableContext(), &goai.StreamOptions{})
	cfg := requireThinkingConfig(t, req)
	if cfg.ThinkingBudget == nil || *cfg.ThinkingBudget != 0 || cfg.IncludeThoughts != nil || cfg.ThinkingLevel != "" {
		t.Fatalf("Gemini 2.5 disabled thinking config = %#v", cfg)
	}
}

func TestGoogleThinkingDisableGemini3FlashUsesMinimalLevel(t *testing.T) {
	req := buildRequest(&goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI, Reasoning: true}, googleThinkingDisableContext(), &goai.StreamOptions{})
	cfg := requireThinkingConfig(t, req)
	if cfg.ThinkingLevel != "MINIMAL" || cfg.ThinkingBudget != nil || cfg.IncludeThoughts != nil {
		t.Fatalf("Gemini 3 Flash disabled thinking config = %#v", cfg)
	}
}

func TestGoogleThinkingDisableGemini31ProUsesMinimalLevel(t *testing.T) {
	maxTokens := 512
	req := buildRequest(&goai.Model{ID: "gemini-3.1-pro-preview", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI, Reasoning: true}, googleThinkingDisableContext(), &goai.StreamOptions{MaxTokens: &maxTokens})
	cfg := requireThinkingConfig(t, req)
	if cfg.ThinkingLevel != "LOW" || cfg.ThinkingBudget != nil || cfg.IncludeThoughts != nil {
		t.Fatalf("Gemini 3.1 Pro disabled thinking config = %#v", cfg)
	}
	if req.GenerationConfig.MaxOutputTokens == nil || *req.GenerationConfig.MaxOutputTokens != 512 {
		t.Fatalf("max output tokens not preserved: %#v", req.GenerationConfig.MaxOutputTokens)
	}
}

func googleThinkingDisableContext() *goai.Context {
	return &goai.Context{
		SystemPrompt: "You are a precise assistant. Follow the requested output format exactly.",
		Messages:     []goai.Message{goai.UserMessage("Before replying, carefully solve 36863 * 5279 internally. Then reply with the word pong repeated exactly 40 times, separated by single spaces. Do not add any other text.")},
	}
}

func requireThinkingConfig(t *testing.T, req geminiRequest) *geminiThinkingConfig {
	t.Helper()
	if req.GenerationConfig == nil || req.GenerationConfig.ThinkingConfig == nil {
		t.Fatalf("missing thinking config: %#v", req.GenerationConfig)
	}
	return req.GenerationConfig.ThinkingConfig
}
