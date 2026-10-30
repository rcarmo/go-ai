package openaicodex

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildCodexRequestClampsMaxOutputTokensToContext(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "codex-test", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, ContextWindow: 5000, MaxTokens: 4000}
	req := buildCodexRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	// The effective default instruction is part of the context budget now:
	// 5000 context - 4096 reserve - 2 user tokens - 8 instruction tokens = 894.
	expected := goai.ClampStreamMaxTokensPtr(model, &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if expected == nil || *expected != 894 {
		t.Fatal("default instruction was not budgeted", expected)
	}
	if req.MaxOutputTokens == nil || *req.MaxOutputTokens != *expected {
		t.Fatalf("max_output_tokens=%v, want %v", req.MaxOutputTokens, expected)
	}
}
