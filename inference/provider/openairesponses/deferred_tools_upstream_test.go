package openairesponses

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func deferredTool(name string) goai.Tool {
	return goai.Tool{Name: name, Description: "The " + name + " tool", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func deferredContext(tools []goai.Tool, added []string) *goai.Context {
	return &goai.Context{Tools: tools, Messages: []goai.Message{
		goai.UserMessage("Hello"),
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "base_tool", Arguments: map[string]interface{}{}}}, Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI, Model: "gpt-5.4", Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse, Timestamp: 2},
		{Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "base_tool", Content: []goai.ContentBlock{{Type: "text", Text: "done"}}, AddedToolNames: added, Timestamp: 3},
		goai.UserMessage("next"),
	}}
}

func responsesDeferredModel(id string, provider goai.Provider) *goai.Model {
	return &goai.Model{ID: id, Provider: provider, Api: goai.ApiOpenAIResponses, MaxTokens: 32000, ContextWindow: 200000}
}

func TestUpstreamDeferredToolsOpenAIResponsesToolSearchAndFallbacks(t *testing.T) {
	ctx := deferredContext([]goai.Tool{deferredTool("base_tool"), deferredTool("late_tool")}, []string{"late_tool"})
	req := buildRequest(responsesDeferredModel("gpt-5.4", goai.ProviderOpenAI), ctx, &goai.StreamOptions{})
	if len(req.Tools) != 1 || req.Tools[0].Name != "base_tool" {
		t.Fatalf("tools=%#v, want only base_tool immediate", req.Tools)
	}
	var input []map[string]interface{}
	if err := json.Unmarshal(req.Input, &input); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range input {
		if item["type"] == "tool_search_output" {
			found = true
			tools := item["tools"].([]interface{})
			tool := tools[0].(map[string]interface{})
			if tool["name"] != "late_tool" || tool["defer_loading"] != true {
				t.Fatalf("tool_search_output=%#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("tool_search_output not found in %#v", input)
	}

	unsupported := buildRequest(responsesDeferredModel("gpt-5.2", goai.ProviderOpenAI), ctx, &goai.StreamOptions{})
	if len(unsupported.Tools) != 2 {
		t.Fatalf("unsupported tools=%#v, want normal list", unsupported.Tools)
	}
	var unsupportedInput []map[string]interface{}
	_ = json.Unmarshal(unsupported.Input, &unsupportedInput)
	for _, item := range unsupportedInput {
		if item["type"] == "tool_search_output" {
			t.Fatalf("unsupported model emitted tool search: %#v", unsupportedInput)
		}
	}

	disabled := responsesDeferredModel("gpt-5.4", "openai-proxy")
	f := false
	disabled.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsToolSearch: &f}
	disabledReq := buildRequest(disabled, ctx, &goai.StreamOptions{})
	if len(disabledReq.Tools) != 2 {
		t.Fatalf("disabled tool search tools=%#v", disabledReq.Tools)
	}
}

func TestUpstreamDeferredToolsCodexToolSearchOnlyForSupportedModels(t *testing.T) {
	ctx := deferredContext([]goai.Tool{deferredTool("base_tool"), deferredTool("late_tool")}, []string{"late_tool"})
	supported := responsesDeferredModel("gpt-5.4", goai.ProviderOpenAICodex)
	supported.Api = goai.ApiOpenAICodexResponses
	req := buildRequest(supported, ctx, &goai.StreamOptions{})
	if len(req.Tools) != 1 {
		t.Fatalf("supported codex tools=%#v, want deferred", req.Tools)
	}
	unsupported := responsesDeferredModel("gpt-5.3-codex-spark", goai.ProviderOpenAICodex)
	unsupported.Api = goai.ApiOpenAICodexResponses
	unsupportedReq := buildRequest(unsupported, ctx, &goai.StreamOptions{})
	if len(unsupportedReq.Tools) != 2 {
		t.Fatalf("unsupported codex tools=%#v, want normal list", unsupportedReq.Tools)
	}
}
