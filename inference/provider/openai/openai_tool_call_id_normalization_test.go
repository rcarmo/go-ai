package openai

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const failingPipeToolCallID = "call_pAYbIr76hXIjncD9UE4eGfnS|t5nnb2qYMFWGSsr13fhCd1CaCu3t3qONEPuOudu4HSVEtA8YJSL6FAZUxvoOoD792VIJWl91g87EdqsCWp9krVsdBysQoDaf9lMCLb8BS4EYi4gQd5kBQBYLlgD71PYwvf+TbMD9J9/5OMD42oxSRj8H+vRf78/l2Xla33LWz4nOgsddBlbvabICRs8GHt5C9PK5keFtzyi3lsyVKNlfduK3iphsZqs4MLv4zyGJnvZo/+QzShyk5xnMSQX/f98+aEoNflEApCdEOXipipgeiNWnpFSHbcwmMkZoJhURNu+JEz3xCh1mrXeYoN5o+trLL3IXJacSsLYXDrYTipZZbJFRPAucgbnjYBC+/ZzJOfkwCs+Gkw7EoZR7ZQgJ8ma+9586n4tT4cI8DEhBSZsWMjrCt8dxKg=="

func TestToolCallIDNormalizationOpenAICompletionsPrefilledContextWithLongPipeSeparatedIDs(t *testing.T) {
	ctx := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("Use echo"),
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: failingPipeToolCallID, Name: "echo", Arguments: map[string]any{"message": "hello"}}}, Api: goai.ApiOpenAIResponses, Provider: goai.ProviderGitHubCopilot, Model: "gpt-5.2-codex", StopReason: goai.StopReasonToolUse, Usage: &goai.Usage{}},
		{Role: goai.RoleToolResult, ToolCallID: failingPipeToolCallID, ToolName: "echo", Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}},
		goai.UserMessage("Say hi"),
	}}
	model := &goai.Model{ID: "openai/gpt-5.2-codex", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions}
	messages := convertMessages(model, ctx, &goai.OpenAICompletionsCompat{})
	var assistantID, toolID string
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			assistantID = msg.ToolCalls[0].ID
		}
		if msg.Role == "tool" {
			toolID = msg.ToolCallID
		}
	}
	if assistantID != "call_pAYbIr76hXIjncD9UE4eGfnS" {
		t.Fatalf("assistant tool call id=%q", assistantID)
	}
	if toolID != "call_pAYbIr76hXIjncD9UE4eGfnS" {
		t.Fatalf("tool result id=%q", toolID)
	}
}
