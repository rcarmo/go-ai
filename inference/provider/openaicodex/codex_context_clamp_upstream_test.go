package openaicodex

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildCodexRequestClampsMaxOutputTokensToContext(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "codex-test", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, ContextWindow: 5000, MaxTokens: 4000}
	req := buildCodexRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	// No synthetic system instruction is sent: 5000 - 4096 - 2 = 902.
	expected := goai.ClampStreamMaxTokensPtr(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if expected == nil || *expected != 902 {
		t.Fatal("unexpected budget without a default instruction", expected)
	}
	if req.MaxOutputTokens == nil || *req.MaxOutputTokens != *expected {
		t.Fatalf("max_output_tokens=%v, want %v", req.MaxOutputTokens, expected)
	}
}
