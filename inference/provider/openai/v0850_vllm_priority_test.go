package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0850OpenAICompletionsVLLMPrioritySerializes(t *testing.T) {
	priority := 7
	model := &goai.Model{ID: "vllm-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://vllm.example/v1", CompletionsCompat: &goai.OpenAICompletionsCompat{VLLMPriority: &priority}}
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "test"})
	if req.Priority == nil || *req.Priority != 7 {
		t.Fatalf("priority=%v", req.Priority)
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["priority"] != float64(7) {
		t.Fatalf("payload=%s", b)
	}
}

func TestV0850OpenAICompletionsVLLMPriorityOmittedWhenUnset(t *testing.T) {
	model := &goai.Model{ID: "vllm-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://vllm.example/v1"}
	req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "test"})
	if req.Priority != nil {
		t.Fatalf("priority=%v, want nil", req.Priority)
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["priority"]; ok {
		t.Fatalf("payload should omit priority: %s", b)
	}
}
