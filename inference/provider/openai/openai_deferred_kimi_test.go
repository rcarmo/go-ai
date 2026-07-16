package openai

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestKimiDeferredToolsAreIntroducedAfterToolResult(t *testing.T) {
	model := &goai.Model{ID: "kimi-test", Provider: goai.ProviderMoonshotAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.moonshot.ai/v1", CompletionsCompat: &goai.OpenAICompletionsCompat{DeferredToolsMode: "kimi"}}
	baseTool := goai.Tool{Name: "base_tool", Description: "base", Parameters: json.RawMessage(`{"type":"object"}`)}
	lateTool := goai.Tool{Name: "late_tool", Description: "late", Parameters: json.RawMessage(`{"type":"object"}`)}
	ctx := &goai.Context{
		Tools: []goai.Tool{baseTool, lateTool},
		Messages: []goai.Message{
			{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "base_tool", Arguments: map[string]interface{}{"q": "x"}}}},
			{Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "base_tool", AddedToolNames: []string{"late_tool"}, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}},
		},
	}

	req := buildRequestBody(model, ctx, nil)
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "base_tool" {
		t.Fatalf("top-level tools=%#v, want only base_tool", req.Tools)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages=%d, want 3: %#v", len(req.Messages), req.Messages)
	}
	intro := req.Messages[2]
	if intro.Role != "system" || len(intro.Tools) != 1 || intro.Tools[0].Function.Name != "late_tool" || !intro.OmitContent {
		t.Fatalf("deferred intro=%#v, want contentless system late_tool", intro)
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	messages := decoded["messages"].([]interface{})
	introJSON := messages[2].(map[string]interface{})
	if _, ok := introJSON["content"]; ok {
		t.Fatalf("kimi deferred tool system message must omit content: %s", body)
	}
}
