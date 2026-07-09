package openai

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsEmptyToolResultUsesNoToolOutputPlaceholder(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, Input: []string{"text", "image"}}
	ctx := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("Run the command"),
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "tool-1", Name: "bash", Arguments: map[string]interface{}{"command": "true"}}}, Api: model.Api, Provider: model.Provider, Model: model.ID, Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse},
		{Role: goai.RoleToolResult, ToolCallID: "tool-1", ToolName: "bash", Content: []goai.ContentBlock{{Type: "text", Text: ""}}},
	}}
	messages := convertMessages(model, ctx, &goai.OpenAICompletionsCompat{})
	for _, msg := range messages {
		if msg.Role == "tool" {
			if got, _ := msg.Content.(string); got != "(no tool output)" {
				t.Fatalf("tool content=%#v, want %q; messages=%#v", msg.Content, "(no tool output)", messages)
			}
			return
		}
	}
	t.Fatalf("tool message not found: %#v", messages)
}
