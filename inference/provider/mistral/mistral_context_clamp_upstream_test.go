package mistral

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildMistralRequestClampsMaxTokensToContext(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "mistral-test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, ContextWindow: 5000, MaxTokens: 4000}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if req.MaxTokens == nil || *req.MaxTokens != 902 {
		t.Fatalf("max_tokens=%v, want 902", req.MaxTokens)
	}
}
