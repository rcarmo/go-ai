package openairesponses

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildRequestOmitsDefaultReasoningForGitHubCopilot(t *testing.T) {
	model := &goai.Model{
		ID:        "gpt-4.1",
		Provider:  goai.ProviderGitHubCopilot,
		Api:       goai.ApiOpenAIResponses,
		Reasoning: true,
	}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}

	req := buildRequest(model, ctx, nil)
	if req.Reasoning != nil {
		t.Fatalf("expected reasoning to be omitted, got %#v", req.Reasoning)
	}
	if len(req.Include) != 0 {
		t.Fatalf("expected no include fields, got %#v", req.Include)
	}
}

func TestBuildRequestClampsPromptCacheKey(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{SessionID: strings.Repeat("x", 80), CacheRetention: goai.CacheRetentionShort})
	if got, want := req.PromptCacheKey, strings.Repeat("x", 64); got != want {
		t.Fatalf("prompt cache key = %q, want %q", got, want)
	}
}

func TestBuildRequestDefaultsReasoningForNonCopilotReasoningModels(t *testing.T) {
	model := &goai.Model{
		ID:        "gpt-4.1",
		Provider:  goai.ProviderOpenAI,
		Api:       goai.ApiOpenAIResponses,
		Reasoning: true,
	}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}

	req := buildRequest(model, ctx, nil)
	if req.Reasoning == nil {
		t.Fatal("expected default reasoning block")
	}
	if req.Reasoning.Effort != "medium" {
		t.Fatalf("expected medium effort, got %q", req.Reasoning.Effort)
	}
}
