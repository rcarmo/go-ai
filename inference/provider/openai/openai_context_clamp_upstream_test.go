package openai

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildRequestBodyClampsMaxTokensToContext(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, ContextWindow: 5000, MaxTokens: 4000}
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if req.MaxCompletionToks == nil || *req.MaxCompletionToks != 902 {
		t.Fatalf("max_completion_tokens=%v, want 902", req.MaxCompletionToks)
	}
}
