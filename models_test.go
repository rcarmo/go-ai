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
