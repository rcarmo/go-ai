package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	goaiimages "github.com/rcarmo/go-ai/images"
)

func TestV0850CatalogCountsAndGrokRemoval(t *testing.T) {
	goai.ClearModels()
	t.Cleanup(goai.RegisterBuiltinModels)
	goai.RegisterBuiltinModels()
	models := goai.ListModels("")
	providers := map[goai.Provider]bool{}
	apis := map[goai.Api]bool{}
	for _, model := range models {
		providers[model.Provider] = true
		apis[model.Api] = true
	}
	if len(models) != 1336 || len(providers) != 39 || len(apis) != 9 {
		t.Fatalf("catalog models/providers/apis = %d/%d/%d, want 1336/39/9", len(models), len(providers), len(apis))
	}
	if got := goai.GetModel(goai.ProviderXAI, "grok-4"); got != nil {
		t.Fatalf("xai/grok-4 should be removed in v0.85.0, got %#v", got)
	}
	if got := goai.GetModel(goai.ProviderOpenRouter, "x-ai/grok-4"); got != nil {
		t.Fatalf("openrouter x-ai/grok-4 should be removed in v0.85.0, got %#v", got)
	}
}

func TestV0850CatalogHeadlineDeltas(t *testing.T) {
	goai.RegisterBuiltinModels()
	qwen := requireModel(t, goai.ProviderQwenTokenPlan, "qwen3.8-flash")
	if qwen.Api != goai.ApiOpenAICompletions || !qwen.Reasoning || qwen.ContextWindow <= 0 || qwen.MaxTokens <= 0 {
		t.Fatalf("qwen3.8-flash metadata=%#v", qwen)
	}
	baseten := requireModel(t, goai.ProviderBaseten, "moonshotai/Kimi-K2.7-Code")
	if !reflect.DeepEqual(baseten.Input, []string{"text", "image"}) {
		t.Fatalf("baseten input=%#v, want text/image", baseten.Input)
	}
	fireworks := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/kimi-k3")
	if fireworks.Api != goai.ApiOpenAICompletions || fireworks.BaseURL != "https://api.fireworks.ai/inference/v1" || fireworks.CompletionsCompat == nil {
		t.Fatalf("fireworks adapter metadata=%#v", fireworks)
	}
	copilot := requireModel(t, goai.ProviderGitHubCopilot, "claude-fable-5")
	if copilot.Api != goai.ApiAnthropicMessages || copilot.AnthropicCompat == nil || copilot.AnthropicCompat.ForceAdaptiveThinking == nil || !*copilot.AnthropicCompat.ForceAdaptiveThinking {
		t.Fatalf("copilot fable routing/compat=%#v", copilot)
	}
}

func TestV0850ImageCatalogUnchanged(t *testing.T) {
	goaiimages.ClearImageModels()
	goaiimages.RegisterBuiltinImageModels()
	if got := len(goaiimages.ListImageModels(goaiimages.ImagesProviderOpenRouter)); got != 50 {
		t.Fatalf("image model count=%d, want unchanged 50", got)
	}
}
