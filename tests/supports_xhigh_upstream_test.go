package goai_test

import (
	"reflect"
	"strings"
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

func TestCodex140RemovesUnavailableModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, id := range []string{"gpt-5.5", "gpt-5.3-codex-spark"} {
		if goai.GetModel(goai.ProviderOpenAICodex, id) != nil {
			t.Errorf("1.4.0 Codex catalogue must omit %s", id)
		}
	}
}

func TestSupportsXHighIncludesOnlyMediumHighXHighForOpenAIGPT55Pro(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenAI, "gpt-5.5-pro"), []string{"medium", "high", "xhigh"})
}

func TestSupportsXHighIncludesOnlyMediumHighXHighForOpenRouterGPT55Pro(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenRouter, "openai/gpt-5.5-pro"), []string{"medium", "high", "xhigh"})
}

func TestSupportsXHighMatchesDeepSeekV4ProOnOpenCodeGo130(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenCodeGo, "deepseek-v4-pro"), []string{"off", "high", "max"})
}

func TestSupportsXHighIncludesMinimalLowMediumHighPlusOffForOpenCodeGoKimiK27Code(t *testing.T) {
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenCodeGo, "kimi-k2.7-code"), []string{"off", "minimal", "low", "medium", "high"})
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

func TestSupportsXHighIncludesGPT6AstraFullThinkingRange(t *testing.T) {
	for _, tc := range []struct {
		provider goai.Provider
		id       string
	}{
		{goai.ProviderOpenAI, "gpt-6-astra"},
		{goai.ProviderAzureOpenAI, "gpt-6-astra"},
		{goai.ProviderOpenRouter, "openai/gpt-6-astra"},
	} {
		t.Run(string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			assertThinkingLevels(t, requireModelForThinking(t, tc.provider, tc.id), []string{"low", "medium", "high", "xhigh", "max"})
		})
	}
	assertThinkingLevels(t, requireModelForThinking(t, goai.ProviderOpenAICodex, "gpt-6-astra"), []string{"minimal", "low", "medium", "high", "xhigh", "max"})
}

// max-thinking.test.ts: catalogue and resolver assertions. The wire variants
// are in the Codex provider suite.
func TestSourceV160MaxThinkingLevels(t *testing.T) {
	t.Run("ordinary-opt-in", func(t *testing.T) {
		model := &goai.Model{ID: "ordinary-reasoning", Provider: goai.Provider("test"), Api: goai.ApiOpenAICompletions, BaseURL: "https://example.com/v1", Reasoning: true, ContextWindow: 128_000, MaxTokens: 4096}
		assertThinkingLevels(t, model, strings.Fields("off minimal low medium high"))
		if got := goai.ClampThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingMax)); got != goai.ModelThinkingLevel(goai.ThinkingHigh) {
			t.Errorf("clamped max = %q; want high", got)
		}
	})
	for _, id := range []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-luna", "gpt-6-sol"} {
		t.Run(id, func(t *testing.T) {
			model := requireModelForThinking(t, goai.ProviderOpenAICodex, id)
			for _, level := range []goai.ModelThinkingLevel{goai.ModelThinkingLevel(goai.ThinkingXHigh), goai.ModelThinkingLevel(goai.ThinkingMax)} {
				if mapped := model.ThinkingLevelMap[level]; mapped == nil || *mapped != string(level) {
					t.Errorf("thinking mapping for %q = %v; want explicit %q", level, mapped, level)
				}
			}
			assertThinkingLevels(t, model, strings.Fields("off minimal low medium high xhigh max"))
		})
	}
	t.Run("hole-between-high-and-max", func(t *testing.T) {
		max := "max"
		model := &goai.Model{ID: "high-and-max", Provider: goai.Provider("test"), Api: goai.ApiOpenAICompletions, BaseURL: "https://example.com/v1", Reasoning: true, ContextWindow: 128_000, MaxTokens: 4096, ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingXHigh): nil, goai.ModelThinkingLevel(goai.ThinkingMax): &max}}
		assertThinkingLevels(t, model, strings.Fields("off minimal low medium high max"))
		if got := goai.ClampThinkingLevel(model, goai.ModelThinkingLevel(goai.ThinkingXHigh)); got != goai.ModelThinkingLevel(goai.ThinkingMax) {
			t.Errorf("clamped xhigh = %q; want max across disabled level", got)
		}
	})
}

// supports-xhigh.test.ts: preserve exact lists where specified, and both
// membership and exclusion assertions where the source only checks a subset.
func TestSourceV160SupportsXHigh(t *testing.T) {
	type fixture struct {
		provider                      goai.Provider
		id, exact, contains, excludes string
	}
	fixtures := []fixture{
		{goai.ProviderAnthropic, "claude-opus-4-6", "", "max", "xhigh"},
		{goai.ProviderAnthropic, "claude-opus-4-8", "", "xhigh max", ""},
		{goai.ProviderAnthropic, "claude-opus-5", "", "xhigh max", ""},
		{goai.ProviderAnthropic, "claude-sonnet-4-6", "", "max", "xhigh"},
		{goai.ProviderAnthropic, "claude-sonnet-5", "", "xhigh max", ""},
		{goai.ProviderAnthropic, "claude-fable-5", "", "xhigh max", "off"},
		{goai.ProviderAnthropic, "claude-sonnet-4-5", "", "", "xhigh max"},
		{goai.ProviderOpenAI, "gpt-5.5-pro", "medium high xhigh", "", ""},
		{goai.ProviderOpenRouter, "openai/gpt-5.5-pro", "medium high xhigh", "", ""},
		{goai.ProviderDeepSeek, "deepseek-flash", "off low high max", "", ""},
		{goai.ProviderOpenRouter, "deepseek/deepseek-v4.1-flash", "off low high max", "", ""},
		{goai.ProviderOpenCodeGo, "deepseek-v4.1-flash", "low high max", "", ""},
		{goai.ProviderMoonshotAI, "kimi-k2.7-code", "minimal low medium high", "", ""},
		{goai.ProviderMoonshotAICN, "kimi-k2.7-code", "minimal low medium high", "", ""},
		{goai.ProviderMoonshotAI, "kimi-k3", "low high max", "", ""},
		{goai.ProviderMoonshotAICN, "kimi-k3", "low high max", "", ""},
		{goai.Provider("kimi-coding"), "k3", "low high max", "", ""},
		{goai.Provider("kimi-coding"), "kimi-for-coding-highspeed", "", "", "off"},
		{goai.ProviderOpenCode, "grok-build-0.1", "high", "", ""},
		{goai.ProviderOpenRouter, "deepseek/deepseek-v4-flash", "off high xhigh", "", ""},
		{goai.ProviderOpenRouter, "anthropic/claude-opus-4.6", "", "max", "xhigh"},
		{goai.ProviderAmazonBedrock, "global.anthropic.claude-opus-5", "", "xhigh max", ""},
		{goai.ProviderXAI, "grok-4.6", "low medium high xhigh", "", ""},
		{goai.ProviderAmazonBedrock, "global.anthropic.claude-fable-5", "", "xhigh max", "off"},
	}
	for _, id := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-astra", "gpt-6.2-sol"} {
		fixtures = append(fixtures, fixture{goai.ProviderOpenAICodex, id, "", "xhigh", ""})
	}
	for _, id := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-terra"} {
		fixtures = append(fixtures, fixture{goai.ProviderOpenAI, id, "off low medium high xhigh max", "", ""})
	}
	for _, tc := range fixtures {
		t.Run(string(tc.provider)+"/"+tc.id, func(t *testing.T) {
			model := requireModelForThinking(t, tc.provider, tc.id)
			if tc.exact != "" {
				assertThinkingLevels(t, model, strings.Fields(tc.exact))
			}
			for _, level := range strings.Fields(tc.contains) {
				assertThinkingContains(t, model, level)
			}
			for _, level := range strings.Fields(tc.excludes) {
				assertThinkingNotContains(t, model, level)
			}
		})
	}
	for _, id := range []string{"gpt-6.1-sol", "gpt-6.2-astra", "gpt-6.2-sol"} {
		t.Run("off-unsupported/"+id, func(t *testing.T) {
			for _, provider := range []goai.Provider{goai.ProviderOpenAI, goai.ProviderAzureOpenAI, goai.ProviderOpenAICodex} {
				model := requireModelForThinking(t, provider, id)
				want := "low medium high xhigh max"
				if provider == goai.ProviderOpenAICodex {
					want = "minimal " + want
				}
				assertThinkingLevels(t, model, strings.Fields(want))
				if off, present := model.ThinkingLevelMap[goai.ThinkingOff]; !present || off != nil {
					t.Errorf("%s/%s off = %v (present %t); want explicit null", provider, id, off, present)
				}
			}
		})
	}
	t.Run("terra-off-and-minimal", func(t *testing.T) {
		for _, provider := range []goai.Provider{goai.ProviderOpenAI, goai.ProviderAzureOpenAI, goai.ProviderOpenAICodex} {
			model := requireModelForThinking(t, provider, "gpt-6.1-terra")
			if off := model.ThinkingLevelMap[goai.ThinkingOff]; off == nil || *off != "none" {
				t.Errorf("%s Terra off = %v; want explicit none", provider, off)
			}
			minimal, present := model.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMinimal)]
			if !present || (provider == goai.ProviderOpenAICodex && (minimal == nil || *minimal != "low")) || (provider != goai.ProviderOpenAICodex && minimal != nil) {
				t.Errorf("%s Terra minimal = %v (present %t); want Codex low or explicit null", provider, minimal, present)
			}
		}
	})
	for _, id := range []string{"gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6.2-astra", "gpt-6.2-sol"} {
		t.Run("azure-context/"+id, func(t *testing.T) {
			if model := requireModelForThinking(t, goai.ProviderAzureOpenAI, id); model.ContextWindow != 1_050_000 {
				t.Errorf("Azure contextWindow = %d; want 1050000", model.ContextWindow)
			}
		})
	}
	assertSourceV160ThinkingMetadata(t)
}

func assertSourceV160ThinkingMetadata(t *testing.T) {
	t.Helper()
	assertBaseCost := func(t *testing.T, model *goai.Model, expected goai.ModelCost) {
		t.Helper()
		actual := model.Cost
		actual.Tiers = nil // Base cost comparisons do not replace the separate tier assertions.
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("%s/%s base cost = %+v; want %+v", model.Provider, model.ID, actual, expected)
		}
	}
	for _, tc := range []struct {
		id   string
		cost goai.ModelCost
	}{
		{"claude-opus-5-5", goai.ModelCost{Input: 4, Output: 20, CacheRead: 0.2, CacheWrite: 5}},
		{"claude-sonnet-5-5", goai.ModelCost{Input: 2, Output: 10, CacheRead: 0.1, CacheWrite: 2.5}},
	} {
		t.Run("anthropic-metadata/"+tc.id, func(t *testing.T) {
			model := requireModelForThinking(t, goai.ProviderAnthropic, tc.id)
			assertBaseCost(t, model, tc.cost)
			compat := model.AnthropicCompat
			if model.ContextWindow != 1_000_000 || model.MaxTokens != 128_000 || compat == nil {
				t.Fatalf("Anthropic context/cap/compat = %d/%d/%+v", model.ContextWindow, model.MaxTokens, compat)
			}
			for key, value := range map[string]*bool{
				"forceAdaptiveThinking": compat.ForceAdaptiveThinking, "supportsMidConvoEffort": compat.SupportsMidConvoEffort,
				"supportsMidConvoSystemMessages": compat.SupportsMidConvoSystemMessages, "supportsMidConvoToolChanges": compat.SupportsMidConvoToolChanges,
			} {
				if value == nil || !*value {
					t.Errorf("Anthropic %s = %v; want explicit true", key, value)
				}
			}
			if tc.id == "claude-sonnet-5-5" && (compat.SupportsTemperature == nil || *compat.SupportsTemperature) {
				t.Error("Sonnet supportsTemperature must be explicitly false")
			}
			assertThinkingLevels(t, model, strings.Fields("low medium high xhigh max"))
		})
	}
	for _, tc := range []struct {
		id           string
		cost         goai.ModelCost
		codexContext int
	}{
		{"gpt-6-sol", goai.ModelCost{Input: 2, Output: 10, CacheRead: 0.2, CacheWrite: 2.5}, 272_000},
		{"gpt-6-luna", goai.ModelCost{Input: 0.1, Output: 0.5, CacheRead: 0.01, CacheWrite: 0.125}, 272_000},
		{"gpt-6.1-sol", goai.ModelCost{Input: 2, Output: 10, CacheRead: 0.1, CacheWrite: 2.5}, 500_000},
		{"gpt-6.1-terra", goai.ModelCost{Input: 1, Output: 5, CacheRead: 0.05, CacheWrite: 1.25}, 500_000},
		{"gpt-6.2-astra", goai.ModelCost{Input: 8, Output: 40, CacheRead: 0.4, CacheWrite: 10}, 272_000},
		{"gpt-6.2-sol", goai.ModelCost{Input: 2, Output: 10, CacheRead: 0.1, CacheWrite: 2.5}, 500_000},
	} {
		t.Run("openai-codex-metadata/"+tc.id, func(t *testing.T) {
			for _, provider := range []goai.Provider{goai.ProviderOpenAI, goai.ProviderOpenAICodex} {
				model := requireModelForThinking(t, provider, tc.id)
				assertBaseCost(t, model, tc.cost)
				wantTiers := []goai.ModelCostTier{{InputTokensAbove: 272_000, Input: tc.cost.Input * 2, Output: tc.cost.Output * 1.5, CacheRead: tc.cost.CacheRead * 2, CacheWrite: tc.cost.CacheWrite * 2}}
				if !reflect.DeepEqual(model.Cost.Tiers, wantTiers) {
					t.Errorf("%s cost tiers = %+v; want %+v", provider, model.Cost.Tiers, wantTiers)
				}
				contextWindow := 272_000
				if provider == goai.ProviderOpenAICodex {
					contextWindow = tc.codexContext
				}
				compat := model.ResponsesCompat
				if !reflect.DeepEqual(model.Input, []string{"text", "image"}) || model.ContextWindow != contextWindow || model.MaxTokens != 128_000 || compat == nil {
					t.Fatalf("%s input/context/cap/compat = %v/%d/%d/%+v", provider, model.Input, model.ContextWindow, model.MaxTokens, compat)
				}
				for key, value := range map[string]*bool{
					"supportsAdditionalTools": compat.SupportsAdditionalTools, "supportsMidConvoSystemMessages": compat.SupportsMidConvoSystemMessages,
					"supportsOpenAIGrammarTools": compat.SupportsOpenAIGrammarTools, "supportsToolSearch": compat.SupportsToolSearch,
				} {
					if value == nil || !*value {
						t.Errorf("%s %s = %v; want explicit true", provider, key, value)
					}
				}
			}
		})
	}
}
