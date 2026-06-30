package google

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestBuildGoogleRequestUsesRawMaxOutputTokensWithoutContextClamp(t *testing.T) {
	maxTokens := 2000
	model := &goai.Model{ID: "gemini-test", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI, ContextWindow: 5000, MaxTokens: 4000}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{MaxTokens: &maxTokens})
	if req.GenerationConfig.MaxOutputTokens == nil || *req.GenerationConfig.MaxOutputTokens != 2000 {
		t.Fatalf("maxOutputTokens=%v, want raw 2000", req.GenerationConfig.MaxOutputTokens)
	}
}
