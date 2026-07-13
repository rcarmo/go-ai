package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestRegisterBuiltinModels(t *testing.T) {
	goai.RegisterBuiltinModels()

	// Check we have the current upstream-main catalog scope.
	providers := goai.ListProviders()
	if len(providers) < 35 {
		t.Fatalf("expected at least 35 providers, got %d", len(providers))
	}
	total := 0
	for _, provider := range providers {
		total += len(goai.ListModels(provider))
	}
	if total < 1059 {
		t.Fatalf("expected at least 1059 generated models, got %d", total)
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
	if kimi == nil || kimi.Cost.Input != 0.72 || kimi.Cost.Output != 3.49 || kimi.Cost.CacheRead != 0.159 || kimi.ContextWindow != 262144 || kimi.MaxTokens != 262144 {
		t.Fatalf("expected OpenRouter Kimi K2.7 Code v0.80.5 metadata, got %#v", kimi)
	}
	openRouterGLM52 := goai.GetModel(goai.ProviderOpenRouter, "z-ai/glm-5.2")
	if openRouterGLM52 == nil || openRouterGLM52.Cost.Input != 0.532 || openRouterGLM52.Cost.Output != 1.672 || openRouterGLM52.Cost.CacheRead != 0.0988 || openRouterGLM52.ContextWindow != 1048576 || openRouterGLM52.MaxTokens != 131072 {
		t.Fatalf("expected OpenRouter GLM-5.2 v0.80.6 metadata, got %#v", openRouterGLM52)
	}

	openAIGPT56 := goai.GetModel(goai.ProviderOpenAI, "gpt-5.6")
	if openAIGPT56 == nil || openAIGPT56.Api != goai.ApiOpenAIResponses || openAIGPT56.Cost.Input != 5 || len(openAIGPT56.Cost.Tiers) != 1 || openAIGPT56.ContextWindow != 272000 {
		t.Fatalf("expected OpenAI Responses gpt-5.6 current-main metadata, got %#v", openAIGPT56)
	}
	azureGPT56 := goai.GetModel(goai.ProviderAzureOpenAI, "gpt-5.6")
	if azureGPT56 == nil || azureGPT56.Api != goai.ApiAzureOpenAIResponses || azureGPT56.Cost.CacheWrite != 6.25 || azureGPT56.ContextWindow != 1050000 {
		t.Fatalf("expected Azure OpenAI Responses gpt-5.6 current-main metadata, got %#v", azureGPT56)
	}

	xiaomi := goai.GetModel(goai.ProviderXiaomi, "mimo-v2-flash")
	if xiaomi == nil {
		t.Fatal("expected Xiaomi mimo-v2-flash model")
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
