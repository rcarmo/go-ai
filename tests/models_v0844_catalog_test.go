package goai_test

import (
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	goaiimages "github.com/rcarmo/go-ai/images"
)

func TestV0844CatalogCountsAndProviderAPIs(t *testing.T) {
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
		t.Fatalf("catalog models/providers/apis = %d/%d/%d, want current 1336/39/9", len(models), len(providers), len(apis))
	}
}

func TestV0844CatalogFireworksSetChanges(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, id := range []string{"accounts/fireworks/models/kimi-k2p7-code", "accounts/fireworks/models/kimi-k3", "accounts/fireworks/routers/kimi-k3-fast"} {
		if got := goai.GetModel(goai.ProviderFireworks, id); got == nil {
			t.Fatalf("missing v0.84.4 Fireworks model %s", id)
		}
	}
	if got := goai.GetModel(goai.ProviderFireworks, "accounts/fireworks/routers/kimi-k2p6-turbo"); got != nil {
		t.Fatalf("retired Fireworks router still registered: %#v", got)
	}
}

func TestV0844CloudflareGatewayMirrorsToolCapableWorkersAIModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	gateway := requireModel(t, goai.ProviderCloudflareAIGateway, "workers-ai/@cf/zai-org/glm-5.3")
	workers := requireModel(t, goai.ProviderCloudflareWorkersAI, "@cf/zai-org/glm-5.3")
	if gateway.Api != goai.ApiOpenAICompletions || workers.Api != goai.ApiOpenAICompletions {
		t.Fatalf("api gateway/workers=%q/%q", gateway.Api, workers.Api)
	}
	if gateway.BaseURL != "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat" {
		t.Fatalf("gateway baseURL=%q", gateway.BaseURL)
	}
	if gateway.CompletionsCompat == nil || gateway.CompletionsCompat.SendSessionAffinityHeaders == nil || !*gateway.CompletionsCompat.SendSessionAffinityHeaders {
		t.Fatalf("gateway compat=%#v, want session affinity enabled", gateway.CompletionsCompat)
	}
	if !strings.HasPrefix(gateway.ID, "workers-ai/") || strings.HasPrefix(workers.ID, "workers-ai/") {
		t.Fatalf("gateway/workers IDs=%q/%q", gateway.ID, workers.ID)
	}
	if gateway.ContextWindow != workers.ContextWindow || gateway.MaxTokens != workers.MaxTokens || !reflect.DeepEqual(gateway.Input, workers.Input) {
		t.Fatalf("gateway=%#v workers=%#v", gateway, workers)
	}
}

func TestV0844ZAIAndDeepSeekCatalogDeltas(t *testing.T) {
	zai := requireModel(t, goai.ProviderZAI, "glm-5.3")
	if zai.Api != goai.ApiOpenAICompletions || !zai.Reasoning || zai.ContextWindow != 1000000 || zai.MaxTokens != 131072 {
		t.Fatalf("glm-5.3 metadata=%#v", zai)
	}
	if zai.CompletionsCompat == nil || zai.CompletionsCompat.ThinkingFormat != "zai" || zai.CompletionsCompat.ZaiToolStream == nil || !*zai.CompletionsCompat.ZaiToolStream {
		t.Fatalf("glm-5.3 compat=%#v", zai.CompletionsCompat)
	}
	vision := requireModel(t, goai.ProviderDeepSeek, "deepseek-v4-flash-vision-exp")
	if vision.Api != goai.ApiOpenAICompletions || !reflect.DeepEqual(vision.Input, []string{"text", "image"}) || !vision.Reasoning {
		t.Fatalf("deepseek vision metadata=%#v", vision)
	}
}

func TestV0844ImageCatalogAddsMuseAndRecraftV4Models(t *testing.T) {
	goaiimages.RegisterBuiltinImageModels()
	models := goaiimages.ListImageModels(goaiimages.ImagesProviderOpenRouter)
	if len(models) != 50 {
		t.Fatalf("image model count=%d, want 50", len(models))
	}
	for _, id := range []string{"meta/muse-image", "recraft/recraft-v4", "recraft/recraft-v4-vector"} {
		if model := goaiimages.GetImageModel(goaiimages.ImagesProviderOpenRouter, id); model == nil {
			t.Fatalf("missing image model %s", id)
		}
	}
}
