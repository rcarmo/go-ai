package openairesponses

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAIResponsesEmptyToolResultUsesNoToolOutputPlaceholder(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI, Input: []string{"text", "image"}}
	assistant := goai.Message{
		Role:    goai.RoleAssistant,
		Content: []goai.ContentBlock{{Type: "toolCall", ID: "tool-1", Name: "bash", Arguments: map[string]interface{}{"command": "true"}}},
		Api:     model.Api, Provider: model.Provider, Model: model.ID,
		Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse,
	}
	ctx := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("Run the command"),
		assistant,
		{Role: goai.RoleToolResult, ToolCallID: "tool-1", ToolName: "bash", Content: []goai.ContentBlock{{Type: "text", Text: ""}}},
	}}

	input := convertMessages(model, ctx)
	var output string
	for _, item := range input {
		m, ok := item.(map[string]interface{})
		if ok && m["type"] == "function_call_output" {
			output, _ = m["output"].(string)
			break
		}
	}
	if output != "(no tool output)" {
		t.Fatalf("function_call_output output=%q, want %q; input=%#v", output, "(no tool output)", input)
	}
	if strings.Contains(output, "see attached image") {
		t.Fatalf("function_call_output output contains image text: %q", output)
	}
}
