package anthropic

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicForceAdaptiveThinkingSendsLegacyThinkingPayloadForCustomModelIDsByDefault(t *testing.T) {
	reasoning := goai.ThinkingMedium
	payload := buildRequest(makeForceAdaptiveCustomModel(nil), &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, &goai.StreamOptions{Reasoning: &reasoning})
	if payload.Thinking == nil || payload.Thinking.Type != "enabled" || payload.OutputConfig != nil {
		t.Fatalf("thinking=%#v output_config=%#v, want enabled/nil", payload.Thinking, payload.OutputConfig)
	}
}

func TestAnthropicForceAdaptiveThinkingSendsAdaptivePayloadWhenCompatTrue(t *testing.T) {
	reasoning := goai.ThinkingMedium
	payload := buildRequest(makeForceAdaptiveCustomModel(&goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(true)}), &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, &goai.StreamOptions{Reasoning: &reasoning})
	if payload.Thinking == nil || payload.Thinking.Type != "adaptive" || payload.Thinking.Display != "summarized" || payload.OutputConfig == nil || payload.OutputConfig.Effort != "medium" {
		t.Fatalf("thinking=%#v output_config=%#v, want adaptive summarized + medium", payload.Thinking, payload.OutputConfig)
	}
}

func TestAnthropicForceAdaptiveThinkingUsesAdaptiveThinkingWithNativeXHighEffortForClaudeFable5(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderAnthropic, "claude-fable-5")
	if model == nil {
		t.Fatal("missing claude-fable-5")
	}
	reasoning := goai.ThinkingXHigh
	payload := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, &goai.StreamOptions{Reasoning: &reasoning})
	if payload.Thinking == nil || payload.Thinking.Type != "adaptive" || payload.Thinking.Display != "summarized" || payload.OutputConfig == nil || payload.OutputConfig.Effort != "xhigh" {
		t.Fatalf("thinking=%#v output_config=%#v, want adaptive summarized + xhigh", payload.Thinking, payload.OutputConfig)
	}
}

func TestAnthropicForceAdaptiveThinkingAllowsBuiltInAdaptiveModelsToOptOut(t *testing.T) {
	goai.RegisterBuiltinModels()
	base := goai.GetModel(goai.ProviderAnthropic, "claude-opus-4-8")
	if base == nil {
		t.Fatal("missing claude-opus-4-8")
	}
	model := *base
	model.AnthropicCompat = &goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(false)}
	reasoning := goai.ThinkingMedium
	payload := buildRequest(&model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, &goai.StreamOptions{Reasoning: &reasoning})
	if payload.Thinking == nil || payload.Thinking.Type != "enabled" || payload.OutputConfig != nil {
		t.Fatalf("thinking=%#v output_config=%#v, want enabled/nil", payload.Thinking, payload.OutputConfig)
	}
}

func TestAnthropicForceAdaptiveThinkingPreservesDisabledWhenReasoningOff(t *testing.T) {
	payload := buildRequest(makeForceAdaptiveCustomModel(&goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(true)}), &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, &goai.StreamOptions{})
	if payload.Thinking == nil || payload.Thinking.Type != "disabled" || payload.OutputConfig != nil {
		t.Fatalf("thinking=%#v output_config=%#v, want disabled/nil", payload.Thinking, payload.OutputConfig)
	}
}

func makeForceAdaptiveCustomModel(compat *goai.AnthropicMessagesCompat) *goai.Model {
	return &goai.Model{ID: "vendor--claude-opus-latest", Provider: "vendor-proxy", Api: goai.ApiAnthropicMessages, Reasoning: true, MaxTokens: 32000, AnthropicCompat: compat}
}
