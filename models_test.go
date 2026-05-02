package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestRegisterBuiltinModels(t *testing.T) {
	goai.RegisterBuiltinModels()

	// Check we have models
	providers := goai.ListProviders()
	if len(providers) < 10 {
		t.Fatalf("expected at least 10 providers, got %d", len(providers))
	}

	// Check specific well-known models
	tests := []struct {
		provider goai.Provider
		id       string
	}{
		{goai.ProviderOpenAI, "gpt-4o"},
		{goai.ProviderAnthropic, "claude-sonnet-4-20250514"},
		{goai.ProviderGoogle, "gemini-2.5-pro"},
	}

	for _, tt := range tests {
		m := goai.GetModel(tt.provider, tt.id)
		if m == nil {
			t.Errorf("model %s/%s not found", tt.provider, tt.id)
			continue
		}
		if m.Api == "" {
			t.Errorf("model %s/%s has empty API", tt.provider, tt.id)
		}
		if m.ContextWindow <= 0 {
			t.Errorf("model %s/%s has no context window", tt.provider, tt.id)
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

	copilot := goai.GetModel(goai.ProviderGitHubCopilot, "claude-sonnet-4")
	if copilot == nil || copilot.Headers["User-Agent"] == "" || copilot.AnthropicCompat == nil || copilot.AnthropicCompat.SupportsEagerToolInputStreaming == nil || *copilot.AnthropicCompat.SupportsEagerToolInputStreaming {
		t.Fatalf("expected Copilot headers and Anthropic compat from generated metadata, got %#v", copilot)
	}

	xiaomi := goai.GetModel(goai.ProviderXiaomi, "mimo-v2-flash")
	if xiaomi == nil {
		t.Fatal("expected Xiaomi mimo-v2-flash model")
	}
}

func TestListModelsFilter(t *testing.T) {
	goai.RegisterBuiltinModels()

	openaiModels := goai.ListModels(goai.ProviderOpenAI)
	if len(openaiModels) < 5 {
		t.Fatalf("expected at least 5 OpenAI models, got %d", len(openaiModels))
	}

	for _, m := range openaiModels {
		if m.Provider != goai.ProviderOpenAI {
			t.Fatalf("expected provider openai, got %s", m.Provider)
		}
	}
}
