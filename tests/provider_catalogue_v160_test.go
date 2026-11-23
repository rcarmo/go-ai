package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 providers.test.ts:94–106. Read real catalogue metadata,
// rather than the defaults computed later while building a request.
func TestSourceV160ProviderConstrainedSamplingMetadata(t *testing.T) {
	goai.RegisterBuiltinModels()
	gpt4o := goai.GetModel(goai.ProviderOpenAI, "gpt-4o")
	if gpt4o == nil || gpt4o.ResponsesCompat == nil {
		t.Fatal("missing OpenAI gpt-4o Responses metadata")
	}
	if flag := gpt4o.ResponsesCompat.SupportsStrictMode; flag == nil || !*flag {
		t.Fatalf("gpt-4o strict metadata: %v; want true", flag)
	}
	if gpt4o.ResponsesCompat.SupportsOpenAIGrammarTools != nil {
		t.Fatal("gpt-4o grammar metadata must be absent")
	}
	gpt54 := goai.GetModel(goai.ProviderOpenAI, "gpt-5.4")
	if gpt54 == nil || gpt54.ResponsesCompat == nil {
		t.Fatal("missing OpenAI gpt-5.4 Responses metadata")
	}
	strict, grammar := gpt54.ResponsesCompat.SupportsStrictMode, gpt54.ResponsesCompat.SupportsOpenAIGrammarTools
	if strict == nil || !*strict || grammar == nil || !*grammar {
		t.Fatal("gpt-5.4 must explicitly support both strict schemas and grammar tools")
	}
	haiku := goai.GetModel(goai.ProviderAnthropic, "claude-haiku-4-5")
	if haiku == nil || haiku.AnthropicCompat == nil || haiku.AnthropicCompat.SupportsStrictTools == nil || !*haiku.AnthropicCompat.SupportsStrictTools {
		t.Fatal("Claude Haiku must explicitly support strict tools")
	}
	claudeCount := 0
	for _, model := range goai.ListModels(goai.ProviderGitHubCopilot) {
		if model.Api != goai.ApiAnthropicMessages {
			continue
		}
		claudeCount++
		if model.AnthropicCompat == nil || model.AnthropicCompat.SupportsStrictTools == nil || !*model.AnthropicCompat.SupportsStrictTools {
			t.Errorf("Copilot model %s must explicitly support strict tools", model.ID)
		}
	}
	if claudeCount == 0 {
		t.Fatal("copilot catalogue contains no Anthropic Messages models")
	}
}
