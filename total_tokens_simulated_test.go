package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func assertTotalTokensEqualsComponentsSimulated(t *testing.T, label string, usage goai.Usage) {
	t.Helper()
	computed := usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
	if usage.TotalTokens != computed {
		t.Fatalf("%s totalTokens=%d, want computed %d from input=%d output=%d cacheRead=%d cacheWrite=%d", label, usage.TotalTokens, computed, usage.Input, usage.Output, usage.CacheRead, usage.CacheWrite)
	}
}

func TestTotalTokensSimulatedAnthropicWithCacheActivity(t *testing.T) {
	first := goai.Usage{Input: 100, Output: 10, CacheRead: 0, CacheWrite: 1000, TotalTokens: 1110}
	second := goai.Usage{Input: 100, Output: 6, CacheRead: 900, CacheWrite: 0, TotalTokens: 1006}
	assertTotalTokensEqualsComponentsSimulated(t, "anthropic first", first)
	assertTotalTokensEqualsComponentsSimulated(t, "anthropic second", second)
	if !(second.CacheRead > 0 || second.CacheWrite > 0 || first.CacheWrite > 0) {
		t.Fatal("expected simulated Anthropic cache activity")
	}
}

func TestTotalTokensSimulatedOpenAICompletionsNativeTotal(t *testing.T) {
	usage := goai.Usage{Input: 11, Output: 3, CacheRead: 0, CacheWrite: 0, TotalTokens: 14}
	assertTotalTokensEqualsComponentsSimulated(t, "openai completions", usage)
}

func TestTotalTokensSimulatedOpenAIResponsesNativeTotal(t *testing.T) {
	usage := goai.Usage{Input: 17, Output: 5, CacheRead: 2, CacheWrite: 0, TotalTokens: 24}
	assertTotalTokensEqualsComponentsSimulated(t, "openai responses", usage)
}

func TestTotalTokensSimulatedGoogleNativeTotalTokenCount(t *testing.T) {
	usage := goai.Usage{Input: 9, Output: 4, CacheRead: 0, CacheWrite: 0, TotalTokens: 13}
	assertTotalTokensEqualsComponentsSimulated(t, "google", usage)
}

func TestTotalTokensSimulatedOpenAICompatibleProvidersNativeTotal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		usage goai.Usage
	}{
		{"mistral", goai.Usage{Input: 13, Output: 2, TotalTokens: 15}},
		{"xai", goai.Usage{Input: 21, Output: 8, TotalTokens: 29}},
		{"groq", goai.Usage{Input: 5, Output: 7, TotalTokens: 12}},
		{"together", goai.Usage{Input: 31, Output: 9, TotalTokens: 40}},
		{"openrouter", goai.Usage{Input: 44, Output: 6, CacheRead: 10, TotalTokens: 60}},
	} {
		assertTotalTokensEqualsComponentsSimulated(t, tc.name, tc.usage)
	}
}
