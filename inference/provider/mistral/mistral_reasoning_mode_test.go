package mistral

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func makeMistralContext() *goai.Context {
	return &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}
}

func TestMistralReasoningModeUsesReasoningEffortForMistralSmall4(t *testing.T) {
	reasoning := goai.ThinkingMedium
	model := &goai.Model{ID: "mistral-small-2603", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{Reasoning: &reasoning})
	if payload.ReasoningEffort != "high" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want high/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeOmitsReasoningControlsForMistralSmall4WhenThinkingOff(t *testing.T) {
	model := &goai.Model{ID: "mistral-small-2603", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), nil)
	if payload.ReasoningEffort != "" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want empty/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeUsesPromptModeForMagistralReasoningModels(t *testing.T) {
	reasoning := goai.ThinkingMedium
	model := &goai.Model{ID: "magistral-medium-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("medium")}}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{Reasoning: &reasoning})
	if payload.PromptMode != "reasoning" || payload.ReasoningEffort != "" {
		t.Fatalf("promptMode=%q reasoningEffort=%q, want reasoning/empty", payload.PromptMode, payload.ReasoningEffort)
	}
}

func TestMistralReasoningModeUsesReasoningEffortForMistralMedium35(t *testing.T) {
	reasoning := goai.ThinkingMedium
	model := &goai.Model{ID: "mistral-medium-3.5", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{Reasoning: &reasoning})
	if payload.ReasoningEffort != "high" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want high/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeOmitsReasoningControlsForMistralMedium35WhenThinkingOff(t *testing.T) {
	model := &goai.Model{ID: "mistral-medium-3.5", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations, Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMedium): strPtr("high")}}
	payload := buildRequest(model, makeMistralContext(), nil)
	if payload.ReasoningEffort != "" || payload.PromptMode != "" {
		t.Fatalf("reasoningEffort=%q promptMode=%q, want empty/empty", payload.ReasoningEffort, payload.PromptMode)
	}
}

func TestMistralReasoningModeUsesSessionIDAsPromptCacheKey(t *testing.T) {
	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{SessionID: "session-123"})
	if payload.PromptCacheKey != "session-123" {
		t.Fatalf("promptCacheKey=%q, want session-123", payload.PromptCacheKey)
	}
}

func TestMistralReasoningModeOmitsPromptCacheKeyWhenCacheRetentionDisabled(t *testing.T) {
	model := &goai.Model{ID: "mistral-large-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	payload := buildRequest(model, makeMistralContext(), &goai.StreamOptions{SessionID: "session-123", CacheRetention: goai.CacheRetentionNone})
	if payload.PromptCacheKey != "" {
		t.Fatalf("promptCacheKey=%q, want empty", payload.PromptCacheKey)
	}
}

func strPtr(s string) *string { return &s }
