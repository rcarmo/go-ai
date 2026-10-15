package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestThinking120PinnedProviderVariants(t *testing.T) {
 goai.RegisterBuiltinModels()
	for _, tc := range []struct {
		provider goai.Provider
		id       string
		levels   []string
	}{
		{goai.ProviderOpenAI, "gpt-6.1-sol", []string{"low", "medium", "high", "xhigh", "max"}},
		{goai.ProviderAzureOpenAI, "gpt-6.1-sol", []string{"low", "medium", "high", "xhigh", "max"}},
		{goai.ProviderOpenAICodex, "gpt-6.1-sol", []string{"minimal", "low", "medium", "high", "xhigh", "max"}},
		{goai.ProviderMoonshotAI, "kimi-k3", []string{"low", "high", "max"}},
		{goai.ProviderMoonshotAICN, "kimi-k3", []string{"low", "high", "max"}},
		{goai.Provider("kimi-coding"), "k3", []string{"low", "high", "max"}},
		{goai.ProviderOpenCodeGo, "deepseek-v4.1-flash", []string{"low", "high", "max"}},
		{goai.ProviderOpenRouter, "deepseek/deepseek-v4.1-flash", []string{"off", "low", "high", "max"}},
	} {
		t.Run(string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			model := goai.GetModel(tc.provider, tc.id)
			if model == nil {
				t.Fatal("pinned model absent")
			}
			assertThinkingLevels(t, model, tc.levels)
		})
	}
}
