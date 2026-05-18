package openairesponses

import (
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
