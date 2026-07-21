package openairesponses

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestXAIUpstreamV0811Grok45UsesResponses(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderXAI, "grok-4.5")
	if model == nil {
		t.Fatal("missing xAI grok-4.5")
	}
	if model.Api != goai.ApiOpenAIResponses {
		t.Fatalf("api=%q, want openai-responses per exact upstream v0.81.1 tag 20be4b18", model.Api)
	}
	if model.ContextWindow != 500000 || model.MaxTokens != 500000 || model.Cost.Input != 2 || model.Cost.Output != 6 || model.Cost.CacheRead != 0.3 {
		t.Fatalf("unexpected xAI grok-4.5 v0.81.1 metadata: %#v", model)
	}
	levels := goai.GetSupportedThinkingLevels(model)
	if hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingMinimal)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingLow)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingMedium)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingHigh)) {
		t.Fatalf("unexpected thinking levels: %#v", levels)
	}
	if model.ResponsesCompat == nil || model.ResponsesCompat.SupportsLongCacheRetention == nil || *model.ResponsesCompat.SupportsLongCacheRetention {
		t.Fatalf("expected xAI responses long cache retention disabled, got %#v", model.ResponsesCompat)
	}
}

func hasThinkingLevel(levels []goai.ModelThinkingLevel, want goai.ModelThinkingLevel) bool {
	for _, level := range levels {
		if level == want {
			return true
		}
	}
	return false
}
