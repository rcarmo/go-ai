package goai_test

import (
	"slices"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV101SingleFieldCompatibilityMetadataRetained(t *testing.T) {
	goai.ClearModels()
	t.Cleanup(goai.RegisterBuiltinModels)
	goai.RegisterBuiltinModels()
	for _, id := range []string{"claude-haiku-4-5", "claude-haiku-4-5-20251001", "claude-opus-4-5", "claude-opus-4-5-20251101", "claude-sonnet-4-5", "claude-sonnet-4-5-20250929"} {
		m := goai.GetModel(goai.ProviderAnthropic, id)
		if m == nil || m.AnthropicCompat == nil || m.AnthropicCompat.SupportsStrictTools == nil || !*m.AnthropicCompat.SupportsStrictTools {
			t.Fatalf("strict-only compat lost for %s: %#v", id, m)
		}
	}
	// Exact session-affinity membership from the pinned published catalog.
	want := map[goai.Provider][]string{
		goai.ProviderOpenCode: {
			"gpt-5",
			"gpt-5-codex",
			"gpt-5-nano",
			"gpt-5.1",
			"gpt-5.1-codex",
			"gpt-5.1-codex-max",
			"gpt-5.1-codex-mini",
			"gpt-5.2",
			"gpt-5.2-codex",
			"gpt-5.3-codex",
			"gpt-5.4",
			"gpt-5.4-mini",
			"gpt-5.4-nano",
			"gpt-5.4-pro",
			"gpt-5.5",
			"gpt-5.5-pro",
			"gpt-5.6-luna",
			"gpt-5.6-sol",
			"gpt-5.6-terra",
			"gpt-6-astra",
			"gpt-6-luna",
			"gpt-6-sol",
			"gpt-6.1-sol",
			"gpt-6.2-astra",
			"gpt-6.2-sol",
			"grok-4.5",
			"grok-4.6",
			"grok-4.7",
			"grok-build-0.1",
			"muse-ember-1.0",
			"muse-spark-1.2",
			"muse-spark-1.3",
			"muse-wonder-1.0",
		},
		goai.ProviderOpenCodeGo: {
			"gpt-5.6-luna",
			"gpt-6-luna",
			"grok-4.7",
			"muse-spark-1.2-contributor",
			"muse-spark-1.3-contributor",
		},
	}
	for _, provider := range []goai.Provider{goai.ProviderOpenCode, goai.ProviderOpenCodeGo} {
		var ids []string
		for _, m := range goai.ListModels(provider) {
			if m.ResponsesCompat == nil || m.ResponsesCompat.SessionAffinityFormat != "openai-nosession" {
				continue
			}
			if m.Api != goai.ApiOpenAIResponses {
				t.Errorf("%s/%s affinity is on unexpected API %q", provider, m.ID, m.Api)
			}
			ids = append(ids, m.ID)
		}
		slices.Sort(ids)
		if !slices.Equal(ids, want[provider]) {
			t.Errorf("%s openai-nosession members = %q; want %q", provider, ids, want[provider])
		}
	}
}
