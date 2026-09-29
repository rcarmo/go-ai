package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0991TypedRegistryCountsAndRepresentativeMetadata(t *testing.T) {
	goai.ClearModels()
	goai.ClearImageModels()
	goai.ClearClassifierModels()
	t.Cleanup(goai.RegisterBuiltinModels)
	t.Cleanup(goai.RegisterBuiltinImageModels)
	t.Cleanup(goai.RegisterBuiltinClassifierModels)
	goai.RegisterBuiltinModels()
	goai.RegisterBuiltinImageModels()
	goai.RegisterBuiltinClassifierModels()

	chatModels := goai.ListModels("")
	chatProviders := map[goai.Provider]bool{}
	chatAPIs := map[goai.Api]bool{}
	for _, model := range chatModels {
		chatProviders[model.Provider] = true
		chatAPIs[model.Api] = true
	}
	if len(chatModels) != 1523 || len(chatProviders) != 41 || len(chatAPIs) != 10 {
		t.Fatalf("chat count/providers/apis = %d/%d/%d, want 1523/41/10", len(chatModels), len(chatProviders), len(chatAPIs))
	}

	imageModels := goai.ListImageModels("")
	imageProviders := map[goai.ImageProvider]bool{}
	imageAPIs := map[goai.ImageApi]bool{}
	for _, model := range imageModels {
		imageProviders[model.Provider] = true
		imageAPIs[model.Api] = true
	}
	if len(imageModels) != 57 || len(imageProviders) != 1 || len(imageAPIs) != 1 {
		t.Fatalf("image count/providers/apis = %d/%d/%d, want 57/1/1", len(imageModels), len(imageProviders), len(imageAPIs))
	}

	classifierModels := goai.ListClassifierModels("")
	classifierProviders := map[goai.ClassifierProvider]bool{}
	classifierAPIs := map[goai.ClassifierApi]bool{}
	for _, model := range classifierModels {
		classifierProviders[model.Provider] = true
		classifierAPIs[model.Api] = true
	}
	if len(classifierModels) != 12 || len(classifierProviders) != 5 || len(classifierAPIs) != 2 {
		t.Fatalf("classifier count/providers/apis = %d/%d/%d, want 12/5/2", len(classifierModels), len(classifierProviders), len(classifierAPIs))
	}
	if total := len(chatModels) + len(imageModels) + len(classifierModels); total != 1592 {
		t.Fatalf("typed model total=%d, want 1592", total)
	}

	gpt := goai.GetModel(goai.ProviderOpenAI, "gpt-6.1-sol")
	if gpt == nil || gpt.Type != "chat" || gpt.Api != goai.ApiOpenAIResponses || gpt.ContextWindow != 272000 || gpt.MaxTokens != 128000 || gpt.Cost.Input != 2 || gpt.Cost.Output != 10 || len(gpt.Cost.Tiers) != 1 || gpt.Cost.Tiers[0].InputTokensAbove != 272000 || gpt.InputLimits == nil || gpt.InputLimits.MaxRequestBytes != 512*1024*1024 || gpt.InputLimits.Images == nil || gpt.InputLimits.Images.MaxPerRequest != 1500 || gpt.InputLimits.Images.Resize == nil || gpt.InputLimits.Images.Resize.MaxBytes != 4718592 {
		t.Fatalf("gpt-6.1-sol metadata=%#v", gpt)
	}

	image := goai.GetImageModel(goai.ImageProviderOpenRouter, "black-forest-labs/flux.2-flex")
	if image == nil || image.Type != "image" || image.Api != goai.ImageApiOpenRouter || image.InputLimits == nil || image.InputLimits.Images == nil || image.InputLimits.Images.Resize == nil || image.InputLimits.Images.Resize.MaxWidth != 2000 || image.InputLimits.Images.Resize.MaxBytes != 4718592 {
		t.Fatalf("image metadata=%#v", image)
	}

	classifier := goai.GetClassifierModel(goai.ClassifierProviderTypeSafe, "jev-latest")
	if classifier == nil || classifier.Type != "classifier" || classifier.Api != goai.ClassifierApiTypeSafeSystemOne || classifier.ContextWindow != 64000 || classifier.BaseURL != "https://api.typesafe.ai/v1/" || classifier.Cost.Input != 0 {
		t.Fatalf("classifier metadata=%#v", classifier)
	}
	cloudflare := goai.GetClassifierModel(goai.ClassifierProviderCloudflareWorkersAI, "typesafe/jev")
	if cloudflare == nil || cloudflare.Api != goai.ClassifierApiCloudflareWorkersAI || cloudflare.BaseURL != "https://api.cloudflare.com/client/v4/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai" {
		t.Fatalf("cloudflare classifier metadata=%#v", cloudflare)
	}
}
