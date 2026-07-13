package anthropic

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
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "base_tool", Arguments: map[string]interface{}{}}}, Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic, Model: "claude-opus-4-6", Usage: &goai.Usage{}, StopReason: goai.StopReasonToolUse, Timestamp: 2},
		{Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "base_tool", Content: []goai.ContentBlock{{Type: "text", Text: "done"}}, AddedToolNames: added, Timestamp: 3},
		goai.UserMessage("next"),
	}}
}

func opusDeferredModel() *goai.Model {
	return &goai.Model{ID: "claude-opus-4-6", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, Input: []string{"text", "image"}, MaxTokens: 32000, ContextWindow: 200000}
}

func TestUpstreamDeferredToolsAnthropicReferencesAndFallbacks(t *testing.T) {
	ctx := deferredContext([]goai.Tool{deferredTool("base_tool"), deferredTool("late_tool")}, []string{"late_tool"})
	req := buildRequest(opusDeferredModel(), ctx, &goai.StreamOptions{})
	if len(req.Tools) != 2 || req.Tools[1].Name != "late_tool" || !req.Tools[1].DeferLoading {
		t.Fatalf("tools=%#v, want late_tool defer_loading", req.Tools)
	}
	content := req.Messages[2].Content.([]anthropicContentBlock)
	refs, ok := content[0].Content.([]map[string]string)
	if !ok || len(refs) != 1 || refs[0]["type"] != "tool_reference" || refs[0]["tool_name"] != "late_tool" {
		t.Fatalf("tool result content=%#v, want late_tool tool_reference", content[0].Content)
	}

	missing := buildRequest(opusDeferredModel(), deferredContext([]goai.Tool{deferredTool("base_tool")}, []string{"late_tool"}), &goai.StreamOptions{})
	if len(missing.Tools) != 1 || missing.Tools[0].DeferLoading || missing.Messages[2].Content.([]anthropicContentBlock)[0].Content != "done" {
		t.Fatalf("missing-tool fallback req=%#v", missing)
	}

	allMarked := buildRequest(opusDeferredModel(), deferredContext([]goai.Tool{deferredTool("late_tool")}, []string{"late_tool"}), &goai.StreamOptions{})
	if len(allMarked.Tools) != 1 || allMarked.Tools[0].DeferLoading {
		t.Fatalf("all-marked fallback tools=%#v", allMarked.Tools)
	}

	unsupported := *opusDeferredModel()
	unsupported.ID = "claude-haiku-4-5"
	unsupportedReq := buildRequest(&unsupported, ctx, &goai.StreamOptions{})
	if len(unsupportedReq.Tools) != 2 || unsupportedReq.Tools[1].DeferLoading {
		t.Fatalf("unsupported model should use normal tools: %#v", unsupportedReq.Tools)
	}
}

func TestUpstreamDeferredToolsAnthropicPreservesSiblingOutputAndOAuthCanonicalization(t *testing.T) {
	ctx := deferredContext([]goai.Tool{deferredTool("base_tool"), deferredTool("late_tool")}, []string{"late_tool"})
	ctx.Messages[1].Content = append(ctx.Messages[1].Content, goai.ContentBlock{Type: "toolCall", ID: "call_2", Name: "base_tool", Arguments: map[string]interface{}{}})
	ctx.Messages[2].Content = []goai.ContentBlock{{Type: "text", Text: "work completed"}, {Type: "image", MimeType: "image/png", Data: "aW1hZ2U="}}
	ctx.Messages = append(ctx.Messages[:3], append([]goai.Message{{Role: goai.RoleToolResult, ToolCallID: "call_2", ToolName: "base_tool", Content: []goai.ContentBlock{{Type: "text", Text: "second result"}}}}, ctx.Messages[3:]...)...)
	req := buildRequest(opusDeferredModel(), ctx, &goai.StreamOptions{})
	content := req.Messages[2].Content.([]anthropicContentBlock)
	if len(content) != 3 || content[0].Type != "tool_result" || content[1].Text != "work completed" || content[2].Source == nil || content[2].Source.Data != "aW1hZ2U=" {
		t.Fatalf("sibling-preserving content=%#v", content)
	}

	oauth := deferredContext([]goai.Tool{deferredTool("base_tool"), deferredTool("read")}, []string{"Read"})
	oauthReq := buildRequest(opusDeferredModel(), oauth, &goai.StreamOptions{APIKey: "sk-ant-oat-fake"})
	if len(oauthReq.Tools) != 2 || oauthReq.Tools[1].Name != "Read" || !oauthReq.Tools[1].DeferLoading {
		t.Fatalf("oauth canonical marker tools=%#v", oauthReq.Tools)
	}
	oauth.Messages[1].Content = []goai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "Read", Arguments: map[string]interface{}{}}}
	usedReq := buildRequest(opusDeferredModel(), oauth, &goai.StreamOptions{APIKey: "sk-ant-oat-fake"})
	if len(usedReq.Tools) != 2 || usedReq.Tools[1].DeferLoading {
		t.Fatalf("used canonical tool should stay immediate: %#v", usedReq.Tools)
	}

	dedup := &goai.Context{Tools: []goai.Tool{deferredTool("read"), {Name: "Read", Description: "Canonical definition", Parameters: json.RawMessage(`{"type":"object"}`)}}, Messages: []goai.Message{goai.UserMessage("hi")}}
	dedupReq := buildRequest(opusDeferredModel(), dedup, &goai.StreamOptions{APIKey: "sk-ant-oat-fake"})
	if len(dedupReq.Tools) != 1 || dedupReq.Tools[0].Name != "Read" || dedupReq.Tools[0].Description != "Canonical definition" {
		t.Fatalf("dedup tools=%#v", dedupReq.Tools)
	}
}
