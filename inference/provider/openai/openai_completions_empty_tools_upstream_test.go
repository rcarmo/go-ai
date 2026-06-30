package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsEmptyToolsUpstream(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1"}

	t.Run("omits tools field when context tools is empty", func(t *testing.T) {
		req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{}}, &goai.StreamOptions{APIKey: "test"})
		assertJSONHasNoKey(t, req, "tools")
	})

	t.Run("omits tools field when context tools is nil", func(t *testing.T) {
		req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "test"})
		assertJSONHasNoKey(t, req, "tools")
	})

	t.Run("clamps default maxTokens to remaining context", func(t *testing.T) {
		clampModel := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1", ContextWindow: 10000, MaxTokens: 8000}
		req := buildRequestBody(clampModel, &goai.Context{Messages: []goai.Message{goai.UserMessage(stringOfLen(8000))}}, &goai.StreamOptions{APIKey: "test"})
		payload := marshalRequestMap(t, req)
		if _, ok := payload["max_tokens"]; ok {
			t.Fatalf("max_tokens should be omitted, got %#v", payload["max_tokens"])
		}
		if got := payload["max_completion_tokens"]; got != float64(3904) {
			t.Fatalf("max_completion_tokens = %#v, want 3904", got)
		}
	})

	t.Run("clamps explicit maxTokens to remaining context", func(t *testing.T) {
		maxTokens := 7000
		clampModel := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1", ContextWindow: 10000, MaxTokens: 8000}
		req := buildRequestBody(clampModel, &goai.Context{Messages: []goai.Message{goai.UserMessage(stringOfLen(8000))}}, &goai.StreamOptions{APIKey: "test", MaxTokens: &maxTokens})
		payload := marshalRequestMap(t, req)
		if _, ok := payload["max_tokens"]; ok {
			t.Fatalf("max_tokens should be omitted, got %#v", payload["max_tokens"])
		}
		if got := payload["max_completion_tokens"]; got != float64(3904) {
			t.Fatalf("max_completion_tokens = %#v, want 3904", got)
		}
	})

	t.Run("sends explicit maxTokens", func(t *testing.T) {
		maxTokens := 1234
		req := buildRequestBody(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "test", MaxTokens: &maxTokens})
		payload := marshalRequestMap(t, req)
		if _, ok := payload["max_tokens"]; ok {
			t.Fatalf("max_tokens should be omitted, got %#v", payload["max_tokens"])
		}
		if got := payload["max_completion_tokens"]; got != float64(1234) {
			t.Fatalf("max_completion_tokens = %#v, want 1234", got)
		}
	})

	t.Run("emits tools empty array when conversation has tool history", func(t *testing.T) {
		req := buildRequestBody(model, &goai.Context{
			Messages: []goai.Message{
				goai.UserMessage("use the tool"),
				{
					Role:       goai.RoleAssistant,
					Api:        goai.ApiOpenAICompletions,
					Provider:   goai.ProviderOpenAI,
					Model:      "gpt-4o-mini",
					StopReason: goai.StopReasonToolUse,
					Content:    []goai.ContentBlock{{Type: "toolCall", ID: "t1", Name: "noop", Arguments: map[string]interface{}{}}},
				},
				{
					Role:       goai.RoleToolResult,
					ToolCallID: "t1",
					ToolName:   "noop",
					Content:    []goai.ContentBlock{{Type: "text", Text: "done"}},
				},
			},
			Tools: []goai.Tool{},
		}, &goai.StreamOptions{APIKey: "test"})
		payload := marshalRequestMap(t, req)
		tools, ok := payload["tools"].([]interface{})
		if !ok || len(tools) != 0 {
			t.Fatalf("tools = %#v, want empty array", payload["tools"])
		}
	})
}

func assertJSONHasNoKey(t *testing.T, req chatRequest, key string) {
	t.Helper()
	payload := marshalRequestMap(t, req)
	if _, ok := payload[key]; ok {
		t.Fatalf("%s should be omitted, got %#v", key, payload[key])
	}
}

func stringOfLen(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}

func marshalRequestMap(t *testing.T, req chatRequest) map[string]interface{} {
	t.Helper()
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return payload
}
