package anthropic

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func inlineToolV101(name, description string) goai.Tool {
	return goai.Tool{Name: name, Description: description, Parameters: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}},"required":["query"],"additionalProperties":false}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}
}
func inlineContextV101(later bool) *goai.Context {
	first := inlineToolV101("read", "original")
	ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{first}}, {Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "start"}}}}}
	if later {
		ctx.Messages = append(ctx.Messages, goai.Message{Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "read"}, {Name: "old"}}, ToolsAdded: []goai.Tool{inlineToolV101("read", "redefined"), inlineToolV101("late", "new")}})
	}
	return ctx
}
func TestV101AnthropicInlineToolsStablePrefixAndFullDefinitions(t *testing.T) {
	model := &goai.Model{ID: "custom", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic, MaxTokens: 4096}
	model.AnthropicCompat = &goai.AnthropicMessagesCompat{SupportsMidConvoSystemMessages: boolPtrCompat(true), SupportsMidConvoToolChanges: boolPtrCompat(true), SupportsEagerToolInputStreaming: boolPtrCompat(true), SupportsStrictTools: boolPtrCompat(true)}
	initialCapture := captureAnthropicRequestForCompat(t, model, inlineContextV101(false), nil)
	laterCapture := captureAnthropicRequestForCompat(t, model, inlineContextV101(true), nil)
	initialHeaders, initial := initialCapture.Headers, initialCapture.Body
	headers, later := laterCapture.Headers, laterCapture.Body
	if !reflect.DeepEqual(initial["tools"], later["tools"]) {
		t.Fatalf("prefix changed initial=%#v later=%#v", initial["tools"], later["tools"])
	}
	for _, h := range []string{initialHeaders.Get("Anthropic-Beta"), headers.Get("Anthropic-Beta")} {
		if !strings.Contains(h, inlineToolsBeta) || strings.Contains(h, "mid-conversation-tool-changes") {
			t.Fatalf("beta=%q", h)
		}
	}
	tools := later["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("tools=%#v", tools)
	}
	first := tools[0].(map[string]any)
	placeholder := tools[1].(map[string]any)
	if first["description"] != "original" || first["cache_control"] == nil || first["defer_loading"] != nil || placeholder["name"] != "__pi_deferred_placeholder__" || placeholder["defer_loading"] != true || placeholder["cache_control"] != nil {
		t.Fatalf("tools=%#v", tools)
	}
	additions, removals := 0, 0
	for _, raw := range later["messages"].([]any) {
		m := raw.(map[string]any)
		blocks, ok := m["content"].([]any)
		if !ok {
			continue
		}
		for _, raw := range blocks {
			b := raw.(map[string]any)
			switch b["type"] {
			case "tool_addition":
				additions++
				w := b["tool"].(map[string]any)
				d := w["definition"].(map[string]any)
				if w["type"] != "tool_definition" || d["input_schema"] == nil || d["strict"] != true || d["eager_input_streaming"] != true || d["cache_control"] != nil || d["defer_loading"] != nil {
					t.Fatalf("definition=%#v", w)
				}
			case "tool_removal":
				removals++
				w := b["tool"].(map[string]any)
				if w["type"] != "tool_reference" || w["name"] != "old" {
					t.Fatalf("removal=%#v", w)
				}
			}
		}
	}
	if additions != 2 || removals != 1 {
		t.Fatalf("additions=%d removals=%d", additions, removals)
	}
	messages := later["messages"].([]any)
	last := messages[len(messages)-1].(map[string]any)["content"].([]any)
	cache := last[len(last)-1].(map[string]any)["cache_control"].(map[string]any)
	if cache["type"] != "ephemeral" {
		t.Fatalf("inline block cache=%#v", cache)
	}
	removalContext := inlineContextV101(false)
	removalContext.Messages = append(removalContext.Messages, goai.Message{Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "read"}}})
	removed := captureAnthropicRequestForCompat(t, model, removalContext, nil).Body["messages"].([]any)
	block := removed[len(removed)-1].(map[string]any)["content"].([]any)[0].(map[string]any)
	if block["type"] != "tool_removal" || block["cache_control"] == nil {
		t.Fatalf("removal cache=%#v", block)
	}
}
func TestV101AnthropicInlineToolsFallbackAndOAuthNames(t *testing.T) {
	for _, mode := range []string{"no-system", "no-tools", "empty-initial"} {
		t.Run(mode, func(t *testing.T) {
			model := &goai.Model{ID: "custom", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic, MaxTokens: 4096}
			sys, tools := mode != "no-system", mode != "no-tools"
			model.AnthropicCompat = &goai.AnthropicMessagesCompat{SupportsMidConvoSystemMessages: &sys, SupportsMidConvoToolChanges: &tools}
			ctx := inlineContextV101(true)
			if mode == "empty-initial" {
				ctx.Messages[0].ToolsAdded = nil
			}
			capture := captureAnthropicRequestForCompat(t, model, ctx, nil)
			h, p := capture.Headers, capture.Body
			if strings.Contains(h.Get("Anthropic-Beta"), inlineToolsBeta) {
				t.Fatalf("beta=%s", h.Get("Anthropic-Beta"))
			}
			for _, raw := range p["tools"].([]any) {
				if raw.(map[string]any)["name"] == "__pi_deferred_placeholder__" {
					t.Fatal("fallback placeholder")
				}
			}
		})
	}
	model := &goai.Model{ID: "custom", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic, MaxTokens: 4096}
	model.AnthropicCompat = &goai.AnthropicMessagesCompat{SupportsMidConvoSystemMessages: boolPtrCompat(true), SupportsMidConvoToolChanges: boolPtrCompat(true), SupportsStrictTools: boolPtrCompat(true), SupportsEagerToolInputStreaming: boolPtrCompat(true)}
	p := captureAnthropicRequestForCompat(t, model, inlineContextV101(true), &goai.StreamOptions{APIKey: "sk-ant-oat-test"}).Body
	for _, raw := range p["messages"].([]any) {
		m := raw.(map[string]any)
		blocks, ok := m["content"].([]any)
		if !ok {
			continue
		}
		for _, raw := range blocks {
			b := raw.(map[string]any)
			if b["type"] == "tool_addition" {
				d := b["tool"].(map[string]any)["definition"].(map[string]any)
				if d["input_schema"] == nil || d["strict"] != true || d["eager_input_streaming"] != true || d["cache_control"] != nil || d["defer_loading"] != nil || (d["description"] == "redefined" && d["name"] != "Read") {
					t.Fatalf("oauth definition=%#v", d)
				}
			}
		}
	}
}
