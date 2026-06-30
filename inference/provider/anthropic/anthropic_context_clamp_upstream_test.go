package anthropic

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildAnthropicRequestClampsMaxTokensToContext(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, ContextWindow: 5000, MaxTokens: 4000}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if req.MaxTokens != 902 {
		t.Fatalf("max_tokens=%d, want 902", req.MaxTokens)
	}
}
