package anthropic

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropic130ManagedEffortExplicitOff(t *testing.T) {
	enabled := true
	model := &goai.Model{Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, ID: "claude-fable-5-1", Reasoning: true, MaxTokens: 32000, AnthropicCompat: &goai.AnthropicMessagesCompat{SupportsMidConvoEffort: &enabled}}
	context := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi"), {Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, ProviderThinkingLevel: "low", Content: []goai.ContentBlock{{Type: "text", Text: "earlier"}}}}}
	for _, level := range []goai.ThinkingLevel{"", "off"} {
		req := buildRequest(model, context, &goai.StreamOptions{Reasoning: &level})
		if req.Thinking == nil || req.Thinking.Type != "disabled" || req.Thinking.BlockBinding != nil || req.OutputConfig != nil {
			t.Fatal(req.Thinking, req.OutputConfig)
		}
		for _, m := range req.Messages {
			if m.OutputConfig != nil {
				t.Fatal("off emitted effort marker", m)
			}
		}
	}
	req := buildRequest(model, context, &goai.StreamOptions{})
	if req.Thinking == nil || req.Thinking.Type != "adaptive" || req.Thinking.BlockBinding == nil {
		t.Fatal("omitted option lost managed default", req.Thinking)
	}
	model.ThinkingLevelMap = map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil}
	off := goai.ThinkingLevel("off")
	req = buildRequest(model, context, &goai.StreamOptions{Reasoning: &off})
	if req.Thinking.Type != "adaptive" {
		t.Fatal("unsupported off disabled thinking", req.Thinking)
	}
}
