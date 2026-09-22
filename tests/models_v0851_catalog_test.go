package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0851GPT6AstraDirectCatalogAndCompat(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct {
		provider         goai.Provider
		id               string
		api              goai.Api
		expectTier       bool
		expectResponses  bool
		expectExplicitPC bool
		minimalMapsToLow bool
	}{
		{goai.ProviderOpenAI, "gpt-6-astra", goai.ApiOpenAIResponses, true, true, true, false},
		{goai.ProviderOpenAICodex, "gpt-6-astra", goai.ApiOpenAICodexResponses, true, false, false, true},
		{goai.ProviderAzureOpenAI, "gpt-6-astra", goai.ApiAzureOpenAIResponses, false, true, false, false},
	} {
		t.Run(string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			model := goai.GetModel(tc.provider, tc.id)
			if model == nil {
				t.Fatalf("missing model %s/%s", tc.provider, tc.id)
			}
			if model.Api != tc.api || model.ContextWindow != 272000 || model.MaxTokens != 128000 {
				t.Fatalf("model api/context/max mismatch: %#v", model)
			}
			assertV0851GPT6BaseCostInputAndThinking(t, model, tc.minimalMapsToLow)
			if tc.expectTier {
				assertV0851GPT6LongContextTier(t, model)
			} else if len(model.Cost.Tiers) != 0 {
				t.Fatalf("unexpected tier for %s/%s: %#v", model.Provider, model.ID, model.Cost.Tiers)
			}
			if tc.expectResponses {
				if model.ResponsesCompat == nil {
					t.Fatal("missing responses compat")
				}
				if tc.expectExplicitPC && (model.ResponsesCompat.SupportsExplicitPromptCacheMode == nil || !*model.ResponsesCompat.SupportsExplicitPromptCacheMode) {
					t.Fatalf("supportsExplicitPromptCacheMode mismatch: %#v", model.ResponsesCompat)
				}
				if model.Api == goai.ApiOpenAIResponses && (model.ResponsesCompat.SupportsAdditionalTools == nil || !*model.ResponsesCompat.SupportsAdditionalTools || model.ResponsesCompat.SupportsToolSearch == nil || !*model.ResponsesCompat.SupportsToolSearch) {
					t.Fatalf("tool search/additional tools mismatch: %#v", model.ResponsesCompat)
				}
			}
		})
	}
}

func TestV0851GPT6AstraGeneratedWrapperRecords(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, tc := range []struct {
		provider goai.Provider
		id       string
		api      goai.Api
		context  int
		input    float64
		levels   []string
	}{
		{goai.ProviderGitHubCopilot, "gpt-6-astra", goai.ApiOpenAIResponses, 1000000, 10, []string{"low", "medium", "high", "xhigh", "max"}},
		{goai.ProviderOpenRouter, "openai/gpt-6-astra", goai.ApiOpenAICompletions, 1050000, 10, []string{"low", "medium", "high", "xhigh", "max"}},
		{goai.ProviderOpenRouter, "openai/gpt-6-astra:batch", goai.ApiOpenAICompletions, 1050000, 5, []string{"low", "medium", "high", "xhigh", "max"}},
		{goai.ProviderVercelAIGateway, "openai/gpt-6-astra", goai.ApiAnthropicMessages, 1050000, 10, []string{"off", "minimal", "low", "medium", "high", "xhigh"}},
		{goai.ProviderVercelAIGateway, "openai/gpt-6-astra-fast", goai.ApiAnthropicMessages, 1050000, 20, []string{"off", "minimal", "low", "medium", "high", "xhigh"}},
		{goai.ProviderOpenCode, "gpt-6-astra", goai.ApiOpenAIResponses, 1050000, 10, []string{"low", "medium", "high", "xhigh", "max"}},
	} {
		t.Run(string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			model := goai.GetModel(tc.provider, tc.id)
			if model == nil {
				t.Fatalf("missing wrapper model %s/%s", tc.provider, tc.id)
			}
			if model.Api != tc.api || model.ContextWindow != tc.context || model.MaxTokens != 128000 || model.Cost.Input != tc.input {
				t.Fatalf("wrapper metadata mismatch: %#v", model)
			}
			if !reflect.DeepEqual(model.Input, []string{"text", "image"}) {
				t.Fatalf("wrapper input=%#v, want text+image", model.Input)
			}
			assertThinkingLevels(t, model, tc.levels)
		})
	}
}

func assertV0851GPT6BaseCostInputAndThinking(t *testing.T, model *goai.Model, minimalMapsToLow bool) {
	t.Helper()
	if model.Cost.Input != 10 || model.Cost.Output != 50 || model.Cost.CacheRead != 1 || model.Cost.CacheWrite != 12.5 {
		t.Fatalf("model cost mismatch: %#v", model.Cost)
	}
	if !reflect.DeepEqual(model.Input, []string{"text", "image"}) {
		t.Fatalf("model input=%#v, want text+image", model.Input)
	}
	wantMinimal := (*string)(nil)
	if minimalMapsToLow {
		wantMinimal = stringPtrV0851("low")
	}
	want := map[goai.ModelThinkingLevel]*string{
		goai.ThinkingOff: nil,
		goai.ModelThinkingLevel(goai.ThinkingMinimal): wantMinimal,
		goai.ModelThinkingLevel(goai.ThinkingLow):     stringPtrV0851("low"),
		goai.ModelThinkingLevel(goai.ThinkingMedium):  stringPtrV0851("medium"),
		goai.ModelThinkingLevel(goai.ThinkingHigh):    stringPtrV0851("high"),
		goai.ModelThinkingLevel(goai.ThinkingXHigh):   stringPtrV0851("xhigh"),
		goai.ModelThinkingLevel(goai.ThinkingMax):     stringPtrV0851("max"),
	}
	for level, wantValue := range want {
		got, ok := model.ThinkingLevelMap[level]
		if !ok {
			t.Fatalf("missing thinking level %s", level)
		}
		if wantValue == nil {
			if got != nil {
				t.Fatalf("thinking level %s=%#v, want nil", level, got)
			}
		} else if got == nil || *got != *wantValue {
			t.Fatalf("thinking level %s=%#v, want %q", level, got, *wantValue)
		}
	}
}

func assertV0851GPT6LongContextTier(t *testing.T, model *goai.Model) {
	t.Helper()
	if len(model.Cost.Tiers) != 1 || model.Cost.Tiers[0].InputTokensAbove != 272000 || model.Cost.Tiers[0].Input != 20 || model.Cost.Tiers[0].Output != 75 || model.Cost.Tiers[0].CacheRead != 2 || model.Cost.Tiers[0].CacheWrite != 25 {
		t.Fatalf("model long-context tier mismatch: %#v", model.Cost.Tiers)
	}
}

func stringPtrV0851(s string) *string { return &s }
