package goai_test

import (
	"regexp"
	"sort"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicAdaptiveThinkingModelsMarksBuiltInMessagesModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	providers := goai.ListProviders()
	var flagged []string
	for _, provider := range providers {
		for _, model := range goai.ListModels(provider) {
			if model.Api == goai.ApiAnthropicMessages && model.AnthropicCompat != nil && model.AnthropicCompat.ForceAdaptiveThinking != nil && *model.AnthropicCompat.ForceAdaptiveThinking {
				flagged = append(flagged, string(model.Provider)+"/"+model.ID)
			}
		}
	}
	sort.Strings(flagged)
	for _, want := range []string{
		"anthropic/claude-fable-5",
		"anthropic/claude-opus-4-8",
		"cloudflare-ai-gateway/claude-fable-5",
		"opencode/claude-opus-4-8",
		"vercel-ai-gateway/anthropic/claude-opus-4.8",
	} {
		if !containsString(flagged, want) {
			t.Fatalf("flagged adaptive models missing %q in %#v", want, flagged)
		}
	}
	allowed := regexp.MustCompile(`(opus[-.]4[-.][678]|sonnet[-.]4[-.]6|fable[-.]5)`)
	for _, modelID := range flagged {
		if !allowed.MatchString(modelID) {
			t.Fatalf("unexpected adaptive thinking model %q in %#v", modelID, flagged)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
