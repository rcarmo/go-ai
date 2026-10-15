package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0811KimiCodingAdaptiveThinkingCompat(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, id := range []string{"kimi-for-coding", "k3", "kimi-for-coding-highspeed"} {
		m := goai.GetModel(goai.ProviderKimiCoding, id)
		if m == nil || m.Api != goai.ApiOpenAICompletions || m.BaseURL != "https://api.kimi.com/coding/v1" || m.CompletionsCompat == nil {
			t.Fatalf("1.3 Kimi coding protocol mismatch: %#v", m)
		}
	}

	k3 := goai.GetModel(goai.ProviderKimiCoding, "k3")
	if k3.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMax)] == nil || *k3.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMax)] != "max" {
		t.Fatalf("expected kimi-coding/k3 max thinking map, got %#v", k3.ThinkingLevelMap)
	}
	for _, tc := range []struct {
		level goai.ModelThinkingLevel
		want  string
	}{
		{goai.ModelThinkingLevel(goai.ThinkingLow), "low"},
		{goai.ModelThinkingLevel(goai.ThinkingHigh), "high"},
		{goai.ModelThinkingLevel(goai.ThinkingMax), "max"},
	} {
		if mapped := k3.ThinkingLevelMap[tc.level]; mapped == nil || *mapped != tc.want {
			t.Fatalf("expected kimi-coding/k3 level %s to map to %q, got %#v", tc.level, tc.want, k3.ThinkingLevelMap)
		}
	}
	for _, level := range []goai.ModelThinkingLevel{goai.ModelThinkingLevel(goai.ThinkingOff), goai.ModelThinkingLevel(goai.ThinkingMinimal), goai.ModelThinkingLevel(goai.ThinkingMedium), goai.ModelThinkingLevel(goai.ThinkingXHigh)} {
		if mapped, ok := k3.ThinkingLevelMap[level]; !ok || mapped != nil {
			t.Fatalf("expected kimi-coding/k3 level %s explicitly disabled, got %#v", level, k3.ThinkingLevelMap)
		}
	}
}

func TestV0811XAIRemovedModelsAndOpenCodeGoAdditions(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, id := range []string{"grok-3", "grok-3-fast", "grok-4.20-0309-non-reasoning", "grok-4.20-0309-reasoning", "grok-code-fast-1"} {
		if model := goai.GetModel(goai.ProviderXAI, id); model != nil {
			t.Fatalf("expected xai/%s removed in v0.80.10 regenerated catalog, got %#v", id, model)
		}
	}
	grok45 := goai.GetModel(goai.ProviderXAI, "grok-4.5")
	if grok45 == nil || grok45.Api != goai.ApiOpenAIResponses || !grok45.Reasoning || grok45.ContextWindow != 500000 || grok45.MaxTokens != 500000 || grok45.Cost.Input != 2 || grok45.Cost.Output != 6 || grok45.Cost.CacheRead != 0.3 {
		t.Fatalf("expected xai/grok-4.5 responses metadata, got %#v", grok45)
	}
}
