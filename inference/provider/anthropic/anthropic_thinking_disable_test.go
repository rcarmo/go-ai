package anthropic

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicThinkingDisableSendsDisabledForBudgetBasedReasoningModelsWhenThinkingIsOff(t *testing.T) {
	req := captureAnthropicThinkingPayload(t, mustAnthropicModel(t, "claude-sonnet-4-5"), nil)
	assertAnthropicThinking(t, req.Thinking, &anthropicThinking{Type: "disabled"})
	if req.OutputConfig != nil {
		t.Fatalf("output_config = %#v, want omitted", req.OutputConfig)
	}
}

func TestAnthropicThinkingDisableSendsDisabledForAdaptiveReasoningModelsWhenThinkingIsOff(t *testing.T) {
	req := captureAnthropicThinkingPayload(t, mustAnthropicModel(t, "claude-opus-4-6"), nil)
	assertAnthropicThinking(t, req.Thinking, &anthropicThinking{Type: "disabled"})
	if req.OutputConfig != nil {
		t.Fatalf("output_config = %#v, want omitted", req.OutputConfig)
	}
}

func TestAnthropicThinkingDisableSendsDisabledForClaudeOpus48WhenThinkingIsOff(t *testing.T) {
	req := captureAnthropicThinkingPayload(t, mustAnthropicModel(t, "claude-opus-4-8"), nil)
	assertAnthropicThinking(t, req.Thinking, &anthropicThinking{Type: "disabled"})
	if req.OutputConfig != nil {
		t.Fatalf("output_config = %#v, want omitted", req.OutputConfig)
	}
}

func TestAnthropicThinkingDisableOmitsDisabledForClaudeFable5WhenThinkingIsOff(t *testing.T) {
	req := captureAnthropicThinkingPayload(t, mustAnthropicModel(t, "claude-fable-5"), nil)
	if req.Thinking != nil {
		t.Fatalf("thinking = %#v, want omitted", req.Thinking)
	}
	if req.OutputConfig != nil {
		t.Fatalf("output_config = %#v, want omitted", req.OutputConfig)
	}
}

func TestAnthropicThinkingDisableUsesAdaptiveThinkingForClaudeOpus48WhenReasoningIsEnabled(t *testing.T) {
	reasoning := goai.ThinkingHigh
	req := captureAnthropicThinkingPayload(t, mustAnthropicModel(t, "claude-opus-4-8"), &goai.StreamOptions{Reasoning: &reasoning})
	assertAnthropicThinking(t, req.Thinking, &anthropicThinking{Type: "adaptive", Display: "summarized"})
	if req.OutputConfig == nil || req.OutputConfig.Effort != "high" {
		t.Fatalf("output_config = %#v, want effort=high", req.OutputConfig)
	}
}

func TestAnthropicThinkingDisableMapsXHighReasoningToEffortXHighForClaudeOpus48(t *testing.T) {
	reasoning := goai.ThinkingXHigh
	req := captureAnthropicThinkingPayload(t, mustAnthropicModel(t, "claude-opus-4-8"), &goai.StreamOptions{Reasoning: &reasoning})
	assertAnthropicThinking(t, req.Thinking, &anthropicThinking{Type: "adaptive", Display: "summarized"})
	if req.OutputConfig == nil || req.OutputConfig.Effort != "xhigh" {
		t.Fatalf("output_config = %#v, want effort=xhigh", req.OutputConfig)
	}
}

func captureAnthropicThinkingPayload(t *testing.T, model *goai.Model, opts *goai.StreamOptions) anthropicRequest {
	t.Helper()
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}
	if opts == nil {
		opts = &goai.StreamOptions{APIKey: "fake-key"}
	} else {
		copy := *opts
		copy.APIKey = "fake-key"
		opts = &copy
	}
	return buildRequest(model, ctx, opts)
}

func assertAnthropicThinking(t *testing.T, got, want *anthropicThinking) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("thinking = %#v, want %#v", got, want)
	}
}
