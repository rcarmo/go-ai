package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestV101CatalogTypedCountsAndNewClassifierPrices(t *testing.T) {
	goai.ClearModels()
	goai.ClearImageModels()
	goai.ClearClassifierModels()
	t.Cleanup(goai.RegisterBuiltinModels)
	t.Cleanup(goai.RegisterBuiltinImageModels)
	t.Cleanup(goai.RegisterBuiltinClassifierModels)
	goai.RegisterBuiltinModels()
	goai.RegisterBuiltinImageModels()
	goai.RegisterBuiltinClassifierModels()
	// These live typed counts use the shared published catalog expectations.
	for kind, count := range map[goai.ModelType]int{goai.ModelTypeChat: publishedChatModels, goai.ModelTypeImage: publishedImageModels, goai.ModelTypeClassifier: publishedClassifierModels} {
		if got := len(goai.ListTypedModels(kind, "")); got != count {
			t.Fatalf("%s count=%d want=%d", kind, got, count)
		}
	}
	for id, want := range map[string]struct {
		input   float64
		context int
	}{"@cf/cloudflare/clef": {0.24, 65536}, "@cf/cloudflare/clef-flash": {0.038, 24576}} {
		m := goai.GetClassifierModel(goai.ClassifierProviderCloudflareWorkersAI, id)
		if m == nil || m.ContextWindow != want.context || m.Cost.Input != want.input || m.Api != goai.ClassifierApiCloudflareWorkersAI {
			t.Fatalf("classifier %s=%#v", id, m)
		}
	}
	for _, id := range []string{"deepseek-ai/DeepSeek-V4-Pro", "deepseek-ai/DeepSeek-V4-Pro-0813"} {
		if m := goai.GetModel(goai.ProviderTogether, id); m != nil {
			t.Fatalf("removed Together model %s=%#v", id, m)
		}
	}
	// The published Together catalog keeps the dated V4.1 Flash with these explicit nil levels.
	const flashID = "deepseek-ai/DeepSeek-V4.1-Flash-1017"
	flash := goai.GetModel(goai.ProviderTogether, flashID)
	if flash == nil || !flash.Reasoning || len(flash.ThinkingLevelMap) != 3 {
		t.Fatalf("Together %s=%#v", flashID, flash)
	}
	for _, level := range []goai.ModelThinkingLevel{"minimal", "low", "medium"} {
		if got, ok := flash.ThinkingLevelMap[level]; !ok || got != nil {
			t.Errorf("Together %s %s = %v, present=%t; want explicit nil", flashID, level, got, ok)
		}
	}
	for _, id := range []string{"claude-opus-4-6", "claude-sonnet-4-6"} {
		if goai.GetModel(goai.ProviderCloudflareAIGateway, id) == nil {
			t.Fatalf("gateway renamed id missing %s", id)
		}
	}
}
