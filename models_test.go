package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestRegisterBuiltinModels(t *testing.T) {
	goai.RegisterBuiltinModels()

	// Check we have models
	providers := goai.ListProviders()
	if len(providers) < 30 {
		t.Fatalf("expected at least 30 providers, got %d", len(providers))
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
	if v := deepseek.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingXHigh)]; v == nil || *v != "max" {
		t.Fatalf("expected DeepSeek xhigh to map to max, got %#v", v)
	}

	copilot := firstModelMatching(goai.ProviderGitHubCopilot, func(m *goai.Model) bool {
		return m.Api == goai.ApiAnthropicMessages && m.Headers["User-Agent"] != "" && m.AnthropicCompat != nil
	})
	if copilot == nil {
		t.Fatal("expected at least one Copilot Anthropic-compatible model with headers")
	}
	if copilot.AnthropicCompat.SupportsEagerToolInputStreaming == nil || *copilot.AnthropicCompat.SupportsEagerToolInputStreaming {
		t.Fatalf("expected Copilot Anthropic compat eager streaming override, got %#v", copilot.AnthropicCompat)
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
