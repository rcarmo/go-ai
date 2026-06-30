package openaicodex

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildCodexRequestClampsMaxOutputTokensToContext(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "codex-test", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, ContextWindow: 5000, MaxTokens: 4000}
	req := buildCodexRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if req.MaxOutputTokens == nil || *req.MaxOutputTokens != 902 {
		t.Fatalf("max_output_tokens=%v, want 902", req.MaxOutputTokens)
	}
}
