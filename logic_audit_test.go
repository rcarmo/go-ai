package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestTransformMessagesAddsSyntheticResultForTrailingOrphan(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: "openai", Api: goai.ApiOpenAICompletions, Input: []string{"text"}}
	messages := []goai.Message{
		goai.UserMessage("hi"),
		{
			Role: goai.RoleAssistant,
			Content: []goai.ContentBlock{{Type: "toolCall", ID: "tc1", Name: "search", Arguments: map[string]interface{}{"q": "x"}}},
			StopReason: goai.StopReasonToolUse,
			Provider:   goai.ProviderOpenAI,
			Api:        goai.ApiOpenAICompletions,
			Model:      "test",
		},
	}

	out := goai.TransformMessages(messages, model)
	if len(out) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(out))
	}
	last := out[len(out)-1]
	if last.Role != goai.RoleToolResult || last.ToolCallID != "tc1" || !last.IsError {
		t.Fatalf("expected synthetic errored tool result, got %#v", last)
	}
}

func TestTransformMessagesNilModelReturnsInput(t *testing.T) {
	msgs := []goai.Message{goai.UserMessage("hi")}
	out := goai.TransformMessages(msgs, nil)
	if len(out) != 1 || out[0].Role != goai.RoleUser {
		t.Fatalf("unexpected output: %#v", out)
	}
}

func TestApplyToolCallLimitUsesBudgetTrim(t *testing.T) {
	var msgs []interface{}
	for i := 0; i < 5; i++ {
		msgs = append(msgs,
			map[string]interface{}{"type": "function_call", "name": "search", "call_id": i},
			map[string]interface{}{"type": "function_call_output", "output": "this is a fairly long tool output that should count toward the token budget"},
		)
	}

	res := goai.ApplyToolCallLimit(msgs, goai.ToolCallLimitConfig{Limit: 10, SummaryMax: 2000, OutputChars: 30, MaxEstimatedTokens: 40})
	if res.ToolCallBudgetRemoved == 0 {
		t.Fatalf("expected budget-based removals, got %#v", res)
	}
	if res.EstimatedTokensAfter > res.EstimatedTokensBefore {
		t.Fatalf("expected tokens after <= before, got %d > %d", res.EstimatedTokensAfter, res.EstimatedTokensBefore)
	}
	if res.SummaryText == "" {
		t.Fatal("expected summary text after budget trimming")
	}
}
