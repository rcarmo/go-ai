package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestRegisterBuiltinModels(t *testing.T) {
	goai.ClearModels()
	t.Cleanup(goai.RegisterBuiltinModels)
	goai.RegisterBuiltinModels()

	// Check we have the current official upstream release catalog scope.
	providers := goai.ListProviders()
	if len(providers) != 39 {
		t.Fatalf("expected exactly 39 providers from pi-ai v0.85.0 tag 107d79f, got %d", len(providers))
	}
	total := 0
	for _, provider := range providers {
		total += len(goai.ListModels(provider))
	}
	if total != 1336 {
		t.Fatalf("expected exactly 1336 generated models from pi-ai v0.85.0 tag 107d79f, got %d", total)
	}

	// Check representative provider registries without depending on rotating
	// date-stamped upstream model IDs.
	for _, provider := range []goai.Provider{goai.ProviderOpenAI, goai.ProviderAnthropic, goai.ProviderGoogle} {
		providerModels := goai.ListModels(provider)
		if len(providerModels) == 0 {
			t.Fatalf("expected models for provider %s", provider)
		}
		m := providerModels[0]
		if m.Api == "" {
			t.Errorf("model %s/%s has empty API", m.Provider, m.ID)
		}
		if m.ContextWindow <= 0 {
			t.Errorf("model %s/%s has no context window", m.Provider, m.ID)
		}
	}
}

func TestGeneratedModelMetadataParity(t *testing.T) {
	goai.RegisterBuiltinModels()

	deepseek := goai.GetModel(goai.ProviderDeepSeek, "deepseek-v4-pro")
	if deepseek == nil || deepseek.CompletionsCompat == nil || deepseek.CompletionsCompat.ThinkingFormat != "deepseek" {
		t.Fatalf("expected DeepSeek compat thinking format, got %#v", deepseek)
	}
	if deepseek.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingLow)] != nil {
		t.Fatalf("expected DeepSeek low thinking level to be explicitly unsupported")
	}
	if v := deepseek.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMax)]; v == nil || *v != "max" {
		t.Fatalf("expected DeepSeek max to map to max, got %#v", v)
	}

	copilot := firstModelMatching(goai.ProviderGitHubCopilot, func(m *goai.Model) bool {
		return m.Api == goai.ApiAnthropicMessages && m.Headers["User-Agent"] != "" && m.AnthropicCompat != nil && m.AnthropicCompat.SupportsEagerToolInputStreaming != nil && !*m.AnthropicCompat.SupportsEagerToolInputStreaming
	})
	if copilot == nil {
		t.Fatal("expected at least one Copilot Anthropic-compatible model with eager streaming disabled")
	}

	opencodeGo := goai.GetModel(goai.ProviderOpenCodeGo, "deepseek-v4-flash")
	if opencodeGo == nil || opencodeGo.CompletionsCompat == nil || opencodeGo.CompletionsCompat.ThinkingFormat != "deepseek" {
		t.Fatalf("expected OpenCode Go DeepSeek Flash thinking format parity, got %#v", opencodeGo)
	}
	glm52 := goai.GetModel(goai.ProviderOpenCodeGo, "glm-5.2")
	if glm52 == nil || glm52.ContextWindow != 1000000 || glm52.MaxTokens != 131072 {
		t.Fatalf("expected OpenCode Go GLM-5.2 v0.79.7 metadata, got %#v", glm52)
	}
	geminiImage := goai.GetModel(goai.ProviderOpenRouter, "google/gemini-3-pro-image")
	if geminiImage == nil || geminiImage.Cost.CacheWrite != 0.375 || geminiImage.MaxTokens != 32768 {
		t.Fatalf("expected OpenRouter Gemini 3 Pro Image text registry metadata, got %#v", geminiImage)
	}
	fusion := goai.GetModel(goai.ProviderOpenRouter, "openrouter/fusion")
	if fusion == nil || fusion.ContextWindow != 1000000 || fusion.MaxTokens != 30000 {
		t.Fatalf("expected OpenRouter Fusion v0.79.8 metadata, got %#v", fusion)
	}
	mistralLatest := goai.GetModel(goai.ProviderMistral, "mistral-large-latest")
	if mistralLatest == nil || mistralLatest.Cost.CacheRead != 0.05 {
		t.Fatalf("expected Mistral cache-read pricing metadata, got %#v", mistralLatest)
	}

	kimi := goai.GetModel(goai.ProviderOpenRouter, "moonshotai/kimi-k2.7-code")
	if kimi == nil || kimi.Cost.Input != 0.66 || kimi.Cost.Output != 3.4 || kimi.Cost.CacheRead != 0.18 || kimi.ContextWindow != 262144 || kimi.MaxTokens != 235929 {
		t.Fatalf("expected OpenRouter Kimi K2.7 Code v0.84.4 metadata, got %#v", kimi)
	}
	openRouterGLM52 := goai.GetModel(goai.ProviderOpenRouter, "z-ai/glm-5.2")
	if openRouterGLM52 == nil || openRouterGLM52.Cost.Input != 0.966 || openRouterGLM52.Cost.Output != 3.036 || openRouterGLM52.Cost.CacheRead != 0.1932 || openRouterGLM52.ContextWindow != 1048576 || openRouterGLM52.MaxTokens != 131072 {
		t.Fatalf("expected OpenRouter GLM-5.2 v0.85.0 metadata, got %#v", openRouterGLM52)
	}

	for _, tc := range []struct {
		provider goai.Provider
		id       string
	}{
		{goai.ProviderKimiCoding, "k3"},
		{goai.ProviderKimiCoding, "kimi-for-coding-highspeed"},
		{goai.ProviderMoonshotAI, "kimi-k3"},
		{goai.ProviderMoonshotAICN, "kimi-k3"},
		{goai.ProviderOpenRouter, "meta/muse-spark-1.1"},
		{goai.ProviderOpenRouter, "moonshotai/kimi-k3"},
		{goai.ProviderVercelAIGateway, "moonshotai/kimi-k3"},
		{goai.ProviderVercelAIGateway, "thinkingmachines/inkling"},
	} {
		if model := goai.GetModel(tc.provider, tc.id); model == nil {
			t.Fatalf("expected retained generated catalog model %s/%s", tc.provider, tc.id)
		}
	}
	kimiK3 := goai.GetModel(goai.ProviderMoonshotAI, "kimi-k3")
	if kimiK3.Api != goai.ApiOpenAICompletions || kimiK3.ContextWindow != 1048576 || kimiK3.MaxTokens != 131072 || kimiK3.Cost.Input != 3 || kimiK3.Cost.Output != 15 || kimiK3.Cost.CacheRead != 0.3 || kimiK3.CompletionsCompat == nil || kimiK3.CompletionsCompat.DeferredToolsMode != "kimi" {
		t.Fatalf("expected Moonshot Kimi K3 v0.80.10 metadata, got %#v", kimiK3)
	}
	vercelInkling := goai.GetModel(goai.ProviderVercelAIGateway, "thinkingmachines/inkling")
	if vercelInkling.Api != goai.ApiAnthropicMessages || vercelInkling.ContextWindow != 256000 || vercelInkling.MaxTokens != 256000 || vercelInkling.BaseURL == "" {
		t.Fatalf("expected Vercel Inkling v0.80.9 metadata, got %#v", vercelInkling)
	}
	for _, tc := range []struct {
		provider goai.Provider
		id       string
	}{
		{goai.ProviderOpenRouter, "poolside/laguna-s-2.1"},
		{goai.ProviderOpenRouter, "poolside/laguna-s-2.1:free"},
		{goai.ProviderVercelAIGateway, "poolside/laguna-s-2.1"},
		{goai.ProviderVercelAIGateway, "poolside/laguna-s-2.1-free"},
	} {
		if model := goai.GetModel(tc.provider, tc.id); model == nil || model.ContextWindow == 0 || model.MaxTokens == 0 {
			t.Fatalf("expected v0.81.1 Laguna model %s/%s, got %#v", tc.provider, tc.id, model)
		}
	}

	if openAIGPT56 := goai.GetModel(goai.ProviderOpenAI, "gpt-5.6"); openAIGPT56 != nil {
		t.Fatalf("openai/gpt-5.6 umbrella ID is not present in exact upstream v0.80.7 provider maps, got %#v", openAIGPT56)
	}
	if azureGPT56 := goai.GetModel(goai.ProviderAzureOpenAI, "gpt-5.6"); azureGPT56 != nil {
		t.Fatalf("azure-openai-responses/gpt-5.6 umbrella ID is not present in exact upstream v0.80.7 provider maps, got %#v", azureGPT56)
	}

	for _, id := range []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra"} {
		if m := goai.GetModel(goai.ProviderOpenAI, id); m == nil || m.Api != goai.ApiOpenAIResponses || !m.Reasoning {
			t.Fatalf("expected OpenAI split GPT-5.6 v0.80.7 model %s with responses/reasoning metadata, got %#v", id, m)
		}
		if m := goai.GetModel(goai.ProviderAzureOpenAI, id); m == nil || m.Api != goai.ApiAzureOpenAIResponses || !m.Reasoning {
			t.Fatalf("expected Azure OpenAI split GPT-5.6 v0.80.7 model %s with responses/reasoning metadata, got %#v", id, m)
		}
	}

	xiaomi := goai.GetModel(goai.ProviderXiaomi, "mimo-v2.5")
	if xiaomi == nil {
		t.Fatal("expected Xiaomi mimo-v2.5 model")
	}
	if xiaomi.CompletionsCompat == nil || xiaomi.CompletionsCompat.ThinkingFormat != "deepseek" || xiaomi.CompletionsCompat.RequiresReasoningContentOnAssistantMessages == nil || !*xiaomi.CompletionsCompat.RequiresReasoningContentOnAssistantMessages {
		t.Fatalf("expected Xiaomi OpenAI-compatible DeepSeek thinking compat metadata, got %#v", xiaomi.CompletionsCompat)
	}
}

func TestListModelsFilter(t *testing.T) {
	goai.RegisterBuiltinModels()

	openaiModels := goai.ListModels(goai.ProviderOpenAI)
	if len(openaiModels) < 20 {
		t.Fatalf("expected at least 20 OpenAI models, got %d", len(openaiModels))
	}

	for _, m := range openaiModels {
		if m.Provider != goai.ProviderOpenAI {
			t.Fatalf("expected provider openai, got %s", m.Provider)
		}
	}
}

func firstModelMatching(provider goai.Provider, pred func(*goai.Model) bool) *goai.Model {
	for _, m := range goai.ListModels(provider) {
		if pred(m) {
			return m
		}
	}
	return nil
}
