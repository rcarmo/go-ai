package goai_test

import (
	"math"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUpstreamV0806ThinkingMaxSupportAndClamp(t *testing.T) {
	max := "max"
	model := &goai.Model{Reasoning: true, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{
		goai.ModelThinkingLevel(goai.ThinkingMax): &max,
	}}
	levels := goai.GetSupportedThinkingLevels(model)
	if !containsThinkingLevel(levels, goai.ModelThinkingLevel(goai.ThinkingMax)) {
		t.Fatalf("levels = %#v, want max", levels)
	}
	if got := goai.ClampReasoning(goai.ThinkingMax); got != goai.ThinkingHigh {
		t.Fatalf("ClampReasoning(max) = %q, want high", got)
	}
}

func TestUpstreamV0806CostTierUsesHighestMatchingInputThreshold(t *testing.T) {
	model := &goai.Model{Cost: goai.ModelCost{
		Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 1.25,
		Tiers: []goai.ModelCostTier{
			{InputTokensAbove: 200_000, Input: 4, Output: 5, CacheRead: 0.4, CacheWrite: 4.5},
			{InputTokensAbove: 100_000, Input: 2, Output: 3, CacheRead: 0.2, CacheWrite: 2.5},
		},
	}}
	usage := &goai.Usage{Input: 150_000, Output: 10_000, CacheRead: 10_000, CacheWrite: 5_000}
	cost := goai.CalculateCost(model, usage)
	want := (150_000*2.0 + 10_000*3.0 + 10_000*0.2 + 5_000*2.5) / 1_000_000
	if math.Abs(cost.Total-want) > 1e-9 {
		t.Fatalf("total cost = %v, want %v (%#v)", cost.Total, want, cost)
	}
}

func TestUpstreamV0806ContextEstimateIgnoresUsageBeforeNewerPrefixMessage(t *testing.T) {
	ctx := &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleAssistant, Timestamp: 10, Usage: &goai.Usage{Input: 100, Output: 50}, StopReason: goai.StopReasonStop},
		{Role: goai.RoleUser, Timestamp: 20, Content: []goai.ContentBlock{{Type: "text", Text: "fresh prefix"}}},
		{Role: goai.RoleAssistant, Timestamp: 15, Usage: &goai.Usage{Input: 200, Output: 25}, StopReason: goai.StopReasonStop},
	}}
	est := goai.EstimateContextTokens(ctx)
	if est.LastUsageIndex == nil || *est.LastUsageIndex != 0 {
		t.Fatalf("last usage index = %v, want 0", est.LastUsageIndex)
	}
	if est.UsageTokens != 150 {
		t.Fatalf("usage tokens = %d, want 150", est.UsageTokens)
	}
}

func containsThinkingLevel(levels []goai.ModelThinkingLevel, want goai.ModelThinkingLevel) bool {
	for _, level := range levels {
		if level == want {
			return true
		}
	}
	return false
}
