package anthropic

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicIgnoresAdvancedSamplingParams(t *testing.T) {
	temp := 0.2
	req := buildRequest(&goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, MaxTokens: 1024}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Temperature: &temp, SamplingParams: map[string]any{"top_p": 0.9, "top_k": float64(40), "temperature": 0.8}})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["top_p"]; ok {
		t.Fatalf("anthropic request should ignore top_p sampling param: %s", body)
	}
	if _, ok := payload["top_k"]; ok {
		t.Fatalf("anthropic request should ignore top_k sampling param: %s", body)
	}
}
