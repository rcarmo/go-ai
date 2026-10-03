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
	for kind, count := range map[goai.ModelType]int{goai.ModelTypeChat: 1536, goai.ModelTypeImage: 59, goai.ModelTypeClassifier: 20} {
		if got := len(goai.ListTypedModels(kind, "")); got != count {
			t.Fatalf("%s count=%d want=%d", kind, got, count)
		}
	}
	for id, input := range map[string]float64{"@cf/cloudflare/clef": 0.24, "@cf/cloudflare/clef-flash": 0.09} {
		m := goai.GetClassifierModel(goai.ClassifierProviderCloudflareWorkersAI, id)
		if m == nil || m.ContextWindow != 65536 || m.Cost.Input != input || m.Api != goai.ClassifierApiCloudflareWorkersAI {
			t.Fatalf("classifier %s=%#v", id, m)
		}
	}
	if m := goai.GetModel(goai.ProviderTogether, "deepseek-ai/DeepSeek-V4-Pro"); m != nil {
		t.Fatalf("removed Together model=%#v", m)
	}
	if m := goai.GetModel(goai.ProviderTogether, "deepseek-ai/DeepSeek-V4-Pro-0813"); m == nil || m.ThinkingLevelMap == nil {
		t.Fatalf("replacement Together=%#v", m)
	}
	for _, id := range []string{"claude-opus-4-6", "claude-sonnet-4-6"} {
		if goai.GetModel(goai.ProviderCloudflareAIGateway, id) == nil {
			t.Fatalf("gateway renamed id missing %s", id)
		}
	}
}
