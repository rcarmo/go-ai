package anthropic

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicEagerToolInputCompatSendsPerToolEagerInputStreamingByDefault(t *testing.T) {
	model := &goai.Model{ID: "claude-opus-4-8", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, Reasoning: true, AnthropicCompat: &goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(true)}}
	request := captureAnthropicRequestForCompat(t, model, lookupToolContext(lookupTool()), &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})
	if got := firstAnthropicTool(t, request.Body)["eager_input_streaming"]; got != true {
		t.Fatalf("eager_input_streaming=%#v, want true", got)
	}
	if got := request.Headers.Get("Anthropic-Beta"); got != "" {
		t.Fatalf("Anthropic-Beta=%q, want empty", got)
	}
}

func TestAnthropicEagerToolInputCompatUsesLegacyFineGrainedBetaWhenDisabled(t *testing.T) {
	model := &goai.Model{ID: "claude-opus-4-8", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, Reasoning: true, AnthropicCompat: &goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(true), SupportsEagerToolInputStreaming: boolPtrCompat(false)}}
	request := captureAnthropicRequestForCompat(t, model, lookupToolContext(lookupTool()), &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})
	if _, ok := firstAnthropicTool(t, request.Body)["eager_input_streaming"]; ok {
		t.Fatalf("eager_input_streaming should be omitted: %#v", firstAnthropicTool(t, request.Body))
	}
	if got := request.Headers.Get("Anthropic-Beta"); got != "fine-grained-tool-streaming-2025-05-14" {
		t.Fatalf("Anthropic-Beta=%q, want fine-grained-tool-streaming-2025-05-14", got)
	}
}

func TestAnthropicEagerToolInputCompatNoLegacyBetaWhenNoTools(t *testing.T) {
	model := &goai.Model{ID: "claude-opus-4-8", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, Reasoning: true, AnthropicCompat: &goai.AnthropicMessagesCompat{ForceAdaptiveThinking: boolPtrCompat(true), SupportsEagerToolInputStreaming: boolPtrCompat(false)}}
	request := captureAnthropicRequestForCompat(t, model, lookupToolContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone})
	if _, ok := request.Body["tools"]; ok {
		t.Fatalf("tools should be omitted: %#v", request.Body["tools"])
	}
	if got := request.Headers.Get("Anthropic-Beta"); got != "" {
		t.Fatalf("Anthropic-Beta=%q, want empty", got)
	}
}
