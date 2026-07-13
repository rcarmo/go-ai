package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUpstreamDeferredToolsCountsDefinitionsAfterLatestUsageCheckpoint(t *testing.T) {
	assistant := goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}, Usage: &goai.Usage{Input: 50, Output: 50, TotalTokens: 100}, StopReason: goai.StopReasonStop, Timestamp: 2}
	plain := goai.EstimateContextTokens(&goai.Context{Messages: []goai.Message{assistant, goai.UserMessage("next")}})
	lateTool := goai.Tool{Name: "late_tool", Description: strings.Repeat("x", 4000), Parameters: json.RawMessage(`{"type":"object"}`)}
	marked := goai.EstimateContextTokens(&goai.Context{Messages: []goai.Message{assistant, {Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "base_tool", AddedToolNames: []string{"late_tool"}, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}, Timestamp: 3}}, Tools: []goai.Tool{lateTool}})
	if marked.Tokens <= plain.Tokens+500 || marked.TrailingTokens <= plain.TrailingTokens+500 {
		t.Fatalf("marked estimate=%#v plain=%#v, want added tool definitions counted", marked, plain)
	}
}

func TestUpstreamDeferredToolPlannerLeavesUnsupportedProvidersUnchanged(t *testing.T) {
	ctx := &goai.Context{Tools: []goai.Tool{{Name: "base_tool"}, {Name: "late_tool"}}, Messages: []goai.Message{{Role: goai.RoleToolResult, AddedToolNames: []string{"late_tool"}}}}
	plan := goai.PlanDeferredTools(ctx, false, false)
	if len(plan.Immediate) != 2 || plan.HasDeferred() {
		t.Fatalf("unsupported plan=%#v", plan)
	}
}
