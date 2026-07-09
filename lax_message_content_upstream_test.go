package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUpstreamLaxMessageContentHandlingNormalizesNilContent(t *testing.T) {
	model := &goai.Model{
		ID:            "test-model",
		Name:          "Test Model",
		Api:           goai.ApiOpenAICompletions,
		Provider:      goai.ProviderOpenAI,
		BaseURL:       "https://example.invalid/v1",
		Input:         []string{"text"},
		ContextWindow: 128000,
		MaxTokens:     16000,
	}
	messages := []goai.Message{
		{Role: goai.RoleUser, Content: nil},
		{Role: goai.RoleAssistant, Content: nil, Api: goai.ApiOpenAICompletions, Provider: goai.ProviderOpenAI, Model: "test-model", Usage: &goai.Usage{}, StopReason: goai.StopReasonStop},
		{Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "web_search", Content: nil},
	}

	result := goai.TransformMessages(messages, model)
	if len(result) != 3 {
		t.Fatalf("len(result)=%d, want 3: %#v", len(result), result)
	}
	for i, msg := range result {
		if msg.Content == nil {
			t.Fatalf("result[%d].Content is nil, want empty slice", i)
		}
		if len(msg.Content) != 0 {
			t.Fatalf("result[%d].Content=%#v, want empty slice", i, msg.Content)
		}
	}
}
