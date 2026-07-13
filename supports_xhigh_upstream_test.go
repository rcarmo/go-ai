package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func requireModelForThinking(t *testing.T, provider goai.Provider, id string) *goai.Model {
	t.Helper()
	goai.RegisterBuiltinModels()
	model := goai.GetModel(provider, id)
	if model == nil {
		t.Fatalf("missing model %s/%s", provider, id)
	}
	return model
}

func thinkingLevels(model *goai.Model) []string {
	levels := goai.GetSupportedThinkingLevels(model)
	out := make([]string, len(levels))
	for i, level := range levels {
		out[i] = string(level)
	}
	return out
}

func assertThinkingContains(t *testing.T, model *goai.Model, want string) {
	t.Helper()
	for _, got := range thinkingLevels(model) {
		if got == want {
			return
		}
	}
	t.Fatalf("thinking levels for %s/%s = %#v, want contains %q", model.Provider, model.ID, thinkingLevels(model), want)
}

func assertThinkingNotContains(t *testing.T, model *goai.Model, forbidden string) {
	t.Helper()
	for _, got := range thinkingLevels(model) {
		if got == forbidden {
			t.Fatalf("thinking levels for %s/%s = %#v, want not contains %q", model.Provider, model.ID, thinkingLevels(model), forbidden)
		}
	}
}

func assertThinkingLevels(t *testing.T, model *goai.Model, want []string) {
	t.Helper()
	if got := thinkingLevels(model); !reflect.DeepEqual(got, want) {
		t.Fatalf("thinking levels for %s/%s = %#v, want %#v", model.Provider, model.ID, got, want)
	}
}

func TestSupportsXHighIncludesMaxNotXHighForAnthropicOpus46OnAnthropicMessagesAPI(t *testing.T) {
	model := requireModelForThinking(t, goai.ProviderAnthropic, "claude-opus-4-6")
	assertThinkingContains(t, model, "max")
	assertThinkingNotContains(t, model, "xhigh")
}

func TestSupportsXHighIncludesXHighAndMaxForAnthropicOpus48OnAnthropicMessagesAPI(t *testing.T) {
	model := requireModelForThinking(t, goai.ProviderAnthropic, "claude-opus-4-8")
	assertThinkingContains(t, model, "xhigh")
	assertThinkingContains(t, model, "max")
}

func TestSupportsXHighIncludesXHighButNotOffForAnthropicClaudeFable5(t *testing.T) {
	model := requireModelForThinking(t, goai.ProviderAnthropic, "claude-fable-5")
	assertThinkingContains(t, model, "xhigh")
	assertThinkingNotContains(t, model, "off")
}

func TestSupportsXHighDoesNotIncludeXHighForClaudeSonnet45(t *testing.T) {
	assertThinkingNotContains(t, requireModelForThinking(t, goai.ProviderAnthropic, "claude-sonnet-4-5"), "xhigh")
}

func TestSupportsXHighIncludesXHighForGPT54AndGPT55CodexModels(t *testing.T) {
	for _, modelID := range []string{"gpt-5.4", "gpt-5.5"} {
		assertThinkingContains(t, requireModelForThinking(t, goai.ProviderOpenAICodex, modelID), "xhigh")
	}
}

func TestSupportsXHighIncludesOnlyMediumHighXHighForOpenAIGPT55Pro(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenAI, "gpt-5.5-pro"), []string{"medium", "high", "xhigh"})
}

func TestSupportsXHighIncludesOnlyMediumHighXHighForOpenRouterGPT55Pro(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenRouter, "openai/gpt-5.5-pro"), []string{"medium", "high", "xhigh"})
}

func TestSupportsXHighIncludesOnlyHighMaxPlusOffForDeepSeekV4FlashOnDeepSeek(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderDeepSeek, "deepseek-v4-flash"), []string{"off", "high", "max"})
}

func TestSupportsXHighIncludesOnlyHighMaxPlusOffForDeepSeekV4FlashOnOpenCodeGo(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenCodeGo, "deepseek-v4-flash"), []string{"off", "high", "max"})
}

func TestSupportsXHighIncludesOnlyHighPlusOffForOpenCodeGoKimiK26(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenCodeGo, "kimi-k2.6"), []string{"off", "high"})
}

func TestSupportsXHighExcludesThinkingOffForMoonshotKimiK27CodeModels(t *testing.T) {
	for _, tc := range []struct {
		provider goai.Provider
		id       string
	}{{goai.ProviderMoonshotAI, "kimi-k2.7-code"}, {goai.ProviderMoonshotAICN, "kimi-k2.7-code"}} {
		assertThinkingLevels(t, requireModelForThinking(t, tc.provider, tc.id), []string{"minimal", "low", "medium", "high"})
	}
}

func TestSupportsXHighIncludesOnlyHighForOpenCodeGrokBuild(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenCode, "grok-build-0.1"), []string{"high"})
}

func TestSupportsXHighIncludesOnlyHighXHighPlusOffForDeepSeekV4FlashOnOpenRouter(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenRouter, "deepseek/deepseek-v4-flash"), []string{"off", "high", "xhigh"})
}

func TestSupportsXHighIncludesMaxNotXHighForOpenRouterOpus46OpenAICompletionsAPI(t *testing.T) {
	model := requireModelForThinking(t, goai.ProviderOpenRouter, "anthropic/claude-opus-4.6")
	assertThinkingContains(t, model, "max")
	assertThinkingNotContains(t, model, "xhigh")
}

func TestSupportsXHighIncludesXHighAndMaxButNotOffForBedrockClaudeFable5(t *testing.T) {
	model := requireModelForThinking(t, goai.ProviderAmazonBedrock, "global.anthropic.claude-fable-5")
	assertThinkingContains(t, model, "xhigh")
	assertThinkingContains(t, model, "max")
	assertThinkingNotContains(t, model, "off")
}
