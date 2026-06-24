package openairesponses

import (
	"regexp"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const copilotRawToolCallID = "call_4VnzVawQXPB9MgYib7CiQFEY|I9b95oN1wD/cHXKTw3PpRkL6KkCtzTJhUxMouMWYwHeTo2j3htzfSk7YPx2vifiIM4g3A8XXyOj8q4Bt6SLUG7gqY1E3ELkrkVQNHglRfUmWj84lqxJY+Puieb3VKyX0FB+83TUzn91cDMF/4gzt990IzqVrc+nIb9RRscRD070Du16q1glydVjWR0SBJsE6TbY/esOjFpqplogQqrajm1eI++f3eLi73R6q7hVusY0QbeFySVxABCjhN0lXB04caBe1rzHjYzul6MAXj7uq+0r17VLq+yrtyYhN12wkmFqHeqTyEei6EFPbMy24Nc+IbJlkP0OCg02W+gOnyBFcbi2ctvJFSOhSjt1CqBdqCnnhwUqXjbWiT0wh3DmLScRgTHmGkaI+oAcQQjfic65nxj+TnEkReA=="

func TestOpenAIResponsesForeignToolCallIDHashesForeignCopilotToolItemIDs(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.5", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAIResponses}
	assistant := goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: copilotRawToolCallID, Name: "edit", Arguments: map[string]interface{}{"path": "src/styles/app.css"}}}, Api: goai.ApiOpenAIResponses, Provider: goai.ProviderGitHubCopilot, Model: "gpt-5.5", Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse}
	toolResult := goai.Message{Role: goai.RoleToolResult, ToolCallID: copilotRawToolCallID, ToolName: "edit", Content: []goai.ContentBlock{{Type: "text", Text: "ok"}}}
	ctx := &goai.Context{SystemPrompt: "You are concise.", Messages: []goai.Message{goai.UserMessage("Use the tool."), assistant, toolResult}}
	input := convertMessages(model, ctx)
	var functionCall map[string]interface{}
	for _, item := range input {
		m, ok := item.(map[string]interface{})
		if ok && m["type"] == "function_call" {
			functionCall = m
			break
		}
	}
	if functionCall == nil {
		t.Fatalf("function_call item not found in %#v", input)
	}
	parts := strings.SplitN(copilotRawToolCallID, "|", 2)
	if len(parts) != 2 {
		t.Fatalf("test fixture missing item id separator")
	}
	expected := "fc_" + shortHash(parts[1])
	if got := functionCall["id"]; got != expected {
		t.Fatalf("function_call id=%#v, want %q", got, expected)
	}
	if len(expected) > 64 {
		t.Fatalf("expected id too long: %q", expected)
	}
	if !regexp.MustCompile(`^fc_[A-Za-z0-9]+$`).MatchString(expected) {
		t.Fatalf("expected id shape mismatch: %q", expected)
	}
}
