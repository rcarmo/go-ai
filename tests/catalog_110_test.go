package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestCatalog110Haiku55AdaptiveEffortAndTierPricing(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct {
		provider goai.Provider
		id       string
	}{
		{goai.ProviderAnthropic, "claude-haiku-5-5"},
		{goai.ProviderOpenRouter, "anthropic/claude-haiku-5.5"},
	} {
		model := goai.GetModel(tc.provider, tc.id)
		if model == nil || len(model.Cost.Tiers) == 0 || !model.Reasoning {
			t.Fatal("Haiku5.5/tier missing", tc, model)
		}
		tier := model.Cost.Tiers[len(model.Cost.Tiers)-1]
		usage := &goai.Usage{Input: tier.InputTokensAbove + 1, Output: 100}
		cost := goai.CalculateCost(model, usage)
		want := float64(usage.Input)*tier.Input/1e6 + float64(usage.Output)*tier.Output/1e6
		if cost.Total != want {
			t.Fatal("long prompt priced at base", tc, cost, want)
		}
		if tc.provider == goai.ProviderAnthropic {
			if model.AnthropicCompat == nil || model.AnthropicCompat.ForceAdaptiveThinking == nil || !*model.AnthropicCompat.ForceAdaptiveThinking || model.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMax)] == nil {
				t.Fatal("adaptive/max unavailable", model)
			}
		}
	}
}
