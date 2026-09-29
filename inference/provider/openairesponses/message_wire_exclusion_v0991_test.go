package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0991ThinkingLevelAndNestedCallsRoundTripAndProviderPayloadExclusion(t *testing.T) {
	msg := goai.Message{
		Role:          goai.RoleAssistant,
		Api:           goai.ApiOpenAIResponses,
		Provider:      goai.ProviderOpenAI,
		Model:         "gpt-5.4",
		ThinkingLevel: goai.ModelThinkingLevel(goai.ThinkingHigh),
		Content:       []goai.ContentBlock{{Type: "text", Text: "done"}},
		Usage:         &goai.Usage{},
		StopReason:    goai.StopReasonStop,
	}
	toolResult := goai.Message{
		Role:       goai.RoleToolResult,
		ToolCallID: "call_1",
		ToolName:   "lookup",
		Content:    []goai.ContentBlock{{Type: "text", Text: "result"}},
		NestedCalls: &goai.NestedToolCalls{Complete: true, Calls: []goai.NestedToolCallRecord{{
			ID:             "nested_1",
			Name:           "inner",
			Arguments:      map[string]any{"q": "pi"},
			ArgumentsBytes: 10,
			Status:         "ok",
			DurationMs:     12,
		}}},
	}

	wire, err := json.Marshal([]goai.Message{msg, toolResult})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(wire), `"thinkingLevel":"high"`) || !strings.Contains(string(wire), `"nestedCalls"`) {
		t.Fatalf("message wire did not include new fields: %s", wire)
	}
	var round []goai.Message
	if err := json.Unmarshal(wire, &round); err != nil {
		t.Fatal(err)
	}
	if round[0].ThinkingLevel != goai.ModelThinkingLevel(goai.ThinkingHigh) || round[1].NestedCalls == nil || len(round[1].NestedCalls.Calls) != 1 || round[1].NestedCalls.Calls[0].Name != "inner" {
		t.Fatalf("roundtrip mismatch: %#v", round)
	}

	model := &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi"), msg, toolResult}}, nil)
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "thinkingLevel") || strings.Contains(string(payload), "nestedCalls") || strings.Contains(string(payload), "nested_1") {
		t.Fatalf("provider payload leaked transcript-only fields: %s", payload)
	}
	if !strings.Contains(string(payload), "function_call_output") || !strings.Contains(string(payload), "result") {
		t.Fatalf("provider payload lost normal tool result content: %s", payload)
	}
}
