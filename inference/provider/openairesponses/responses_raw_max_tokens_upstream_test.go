package openairesponses

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildResponsesRequestUsesRawMaxOutputTokensWithoutContextClamp(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, ContextWindow: 5000, MaxTokens: 4000}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if req.MaxOutputTokens == nil || *req.MaxOutputTokens != 2000 {
		t.Fatalf("max_output_tokens=%v, want raw 2000", req.MaxOutputTokens)
	}
}
