package openairesponses

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0850OpenAIResponsesOmitsMaxOutputTokensWhenUnsupported(t *testing.T) {
	max := 123
	unsupported := false
	model := &goai.Model{ID: "resp-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, MaxTokens: 4096, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsMaxOutputTokens: &unsupported}}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{MaxTokens: &max})
	if req.MaxOutputTokens != nil {
		t.Fatalf("MaxOutputTokens=%v, want nil", *req.MaxOutputTokens)
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["max_output_tokens"]; ok {
		t.Fatalf("payload should omit max_output_tokens: %s", b)
	}
}

func TestV0850OpenAIResponsesKeepsMaxOutputTokensByDefault(t *testing.T) {
	max := 123
	model := &goai.Model{ID: "resp-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, MaxTokens: 4096}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{MaxTokens: &max})
	if req.MaxOutputTokens == nil || *req.MaxOutputTokens != 123 {
		t.Fatalf("MaxOutputTokens=%v", req.MaxOutputTokens)
	}
}
