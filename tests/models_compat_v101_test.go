package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
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
	count := 0
	for _, provider := range []goai.Provider{goai.ProviderOpenCode, goai.ProviderOpenCodeGo} {
		for _, m := range goai.ListModels(provider) {
			if m.ResponsesCompat != nil && m.ResponsesCompat.SessionAffinityFormat == "openai-nosession" {
				count++
			}
		}
	}
	if count != 36 {
		t.Fatalf("OpenCode affinity metadata count=%d want36", count)
	}
}
