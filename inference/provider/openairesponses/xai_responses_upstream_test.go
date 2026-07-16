package openairesponses

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestXAIUpstreamV0809Grok45UsesCompletions(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderXAI, "grok-4.5")
	if model == nil {
		t.Fatal("missing xAI grok-4.5")
	}
	if model.Api != goai.ApiOpenAICompletions {
		t.Fatalf("api=%q, want openai-completions per upstream v0.80.9", model.Api)
	}
	levels := goai.GetSupportedThinkingLevels(model)
	if !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingMinimal)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingLow)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingMedium)) || !hasThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingHigh)) {
		t.Fatalf("unexpected thinking levels: %#v", levels)
	}
	compat := goai.DetectCompatForModel(model)
	if compat.SupportsStore == nil || *compat.SupportsStore || compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort || compat.SupportsDeveloperRole == nil || *compat.SupportsDeveloperRole {
		t.Fatalf("unexpected xAI completions compat: %#v", compat)
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
