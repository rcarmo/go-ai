package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func ptrValue[T comparable](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

func requireModel(t *testing.T, provider goai.Provider, id string) *goai.Model {
	t.Helper()
	goai.RegisterBuiltinModels()
	model := goai.GetModel(provider, id)
	if model == nil {
		t.Fatalf("GetModel(%q, %q) is nil", provider, id)
	}
	return model
}

// Source: pi v1.6.0 packages/ai/test/fireworks-models.test.ts:182-187 (key lookup only).
func TestFireworksProviderEnvKeyCompatibilityRetained(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-fireworks-key")
	if got := goai.GetEnvAPIKey(goai.ProviderFireworks); got != "test-fireworks-key" {
		t.Fatal("Fireworks key not resolved from process env")
	}
	env := goai.ProviderEnv{"FIREWORKS_API_KEY": "test-fireworks-scoped-key"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderFireworks, env); got != "test-fireworks-scoped-key" {
		t.Fatal("Fireworks key not resolved from scoped env")
	}
}

// Source: pi v1.6.0 packages/ai/test/together-models.test.ts:16.
func TestTogetherModelsRegistersDefaultKimiK3ViaOpenAICompatibleChatCompletionsAPI(t *testing.T) {
	model := requireModel(t, goai.ProviderTogether, "moonshotai/Kimi-K3")
	if model.Api != goai.ApiOpenAICompletions || model.Provider != goai.ProviderTogether {
		t.Fatalf("api/provider=%q/%q, want openai-completions/together", model.Api, model.Provider)
	}
	if model.BaseURL != "https://api.together.ai/v1" {
		t.Fatalf("baseUrl=%q, want https://api.together.ai/v1", model.BaseURL)
	}
	if !model.Reasoning {
		t.Fatalf("reasoning=false, want true")
	}
	wantThinking := map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil}
	if !reflect.DeepEqual(model.ThinkingLevelMap, wantThinking) {
		t.Fatalf("thinkingLevelMap=%#v, want %#v", model.ThinkingLevelMap, wantThinking)
	}
	if !reflect.DeepEqual(model.Input, []string{"text", "image"}) {
		t.Fatalf("input=%#v, want [text image]", model.Input)
	}
	if model.ContextWindow != 1048576 || model.MaxTokens != 1048576 {
		t.Fatalf("contextWindow/maxTokens=%d/%d, want 1048576/1048576", model.ContextWindow, model.MaxTokens)
	}
	wantCost := goai.ModelCost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 0}
	if !reflect.DeepEqual(model.Cost, wantCost) {
		t.Fatalf("cost=%#v, want %#v", model.Cost, wantCost)
	}
	// The published record omits developer role entirely and sets each of
	// these four options explicitly to false. Nil is not false here.
	disabled := false
	wantCompat := &goai.OpenAICompletionsCompat{
		SupportsStore:              &disabled,
		SupportsReasoningEffort:    &disabled,
		MaxTokensField:             "max_tokens",
		ThinkingFormat:             "together",
		SupportsStrictMode:         &disabled,
		SupportsLongCacheRetention: &disabled,
	}
	if !reflect.DeepEqual(model.CompletionsCompat, wantCompat) {
		t.Fatalf("compat=%#v, want exact upstream Together Kimi compat %#v", model.CompletionsCompat, wantCompat)
	}
}

// Source: pi v1.6.0 packages/ai/test/together-models.test.ts:44 (GPT-OSS and DeepSeek).
// The MiniMax assertion is an existing supplemental catalogue control.
func TestTogetherModelsReasoningControlsFromTogetherAPISurface(t *testing.T) {
	gptOss := requireModel(t, goai.ProviderTogether, "openai/gpt-oss-120b")
	low, medium, high := "low", "medium", "high"
	wantGPTOSS := map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil, goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): &low, goai.ModelThinkingLevel(goai.ThinkingMedium): &medium, goai.ModelThinkingLevel(goai.ThinkingHigh): &high, goai.ModelThinkingLevel(goai.ThinkingMax): nil, goai.ModelThinkingLevel(goai.ThinkingXHigh): nil}
	if !reflect.DeepEqual(gptOss.ThinkingLevelMap, wantGPTOSS) {
		t.Fatalf("gpt-oss thinkingLevelMap=%#v, want %#v", gptOss.ThinkingLevelMap, wantGPTOSS)
	}
	if gptOss.CompletionsCompat == nil || ptrValue(gptOss.CompletionsCompat.SupportsReasoningEffort) != true || gptOss.CompletionsCompat.ThinkingFormat != "openai" {
		t.Fatalf("gpt-oss compat=%#v, want supportsReasoningEffort true and thinkingFormat openai", gptOss.CompletionsCompat)
	}

	deepSeekV4 := requireModel(t, goai.ProviderTogether, "deepseek-ai/DeepSeek-V4-Pro-0813")
	wantDeepSeek := map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil, goai.ModelThinkingLevel(goai.ThinkingHigh): &high, goai.ModelThinkingLevel(goai.ThinkingXHigh): nil}
	if !reflect.DeepEqual(deepSeekV4.ThinkingLevelMap, wantDeepSeek) {
		t.Fatalf("DeepSeek-V4 thinkingLevelMap=%#v, want %#v", deepSeekV4.ThinkingLevelMap, wantDeepSeek)
	}
	if deepSeekV4.CompletionsCompat == nil || ptrValue(deepSeekV4.CompletionsCompat.SupportsReasoningEffort) != true || deepSeekV4.CompletionsCompat.ThinkingFormat != "together" {
		t.Fatalf("DeepSeek-V4 compat=%#v, want supportsReasoningEffort true and thinkingFormat together", deepSeekV4.CompletionsCompat)
	}

	minimax := requireModel(t, goai.ProviderTogether, "MiniMaxAI/MiniMax-M3")
	wantMiniMax := map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil}
	if !reflect.DeepEqual(minimax.ThinkingLevelMap, wantMiniMax) {
		t.Fatalf("MiniMax thinkingLevelMap=%#v, want %#v", minimax.ThinkingLevelMap, wantMiniMax)
	}
	if minimax.CompletionsCompat == nil || minimax.CompletionsCompat.ThinkingFormat != "together" || ptrValue(minimax.CompletionsCompat.SupportsReasoningEffort) != false {
		t.Fatalf("MiniMax compat=%#v, want thinkingFormat together and supportsReasoningEffort false", minimax.CompletionsCompat)
	}
}

func TestTogetherModelsResolvesTogetherAPIKeyFromEnvironment(t *testing.T) {
	env := goai.ProviderEnv{"TOGETHER_API_KEY": "test-together-key"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderTogether, env); got != "test-together-key" {
		t.Fatalf("together env key=%q, want test-together-key", got)
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1215-1225.
func TestXiaomiModelsKeepsMimoV26ProOnAPIBillingAndTokenPlanProviders(t *testing.T) {
	for _, provider := range []goai.Provider{goai.ProviderXiaomi, goai.ProviderXiaomiTokenPlanCN, goai.ProviderXiaomiTokenPlanAMS, goai.ProviderXiaomiTokenPlanSGP} {
		t.Run(string(provider), func(t *testing.T) {
			model := requireModel(t, provider, "mimo-v2.6-pro")
			compat := model.CompletionsCompat
			if compat == nil || compat.RequiresReasoningContentOnAssistantMessages == nil || !*compat.RequiresReasoningContentOnAssistantMessages {
				t.Fatalf("reasoning replay must be explicitly required, got %+v", compat)
			}
			if compat.ThinkingFormat != "deepseek" || compat.MaxTokensField != "" || compat.SupportsDeveloperRole != nil {
				t.Errorf("compat: thinkingFormat=%q maxTokensField=%q supportsDeveloperRole=%v; want deepseek and no token/role override",
					compat.ThinkingFormat, compat.MaxTokensField, compat.SupportsDeveloperRole)
			}
		})
	}
}

// Source: pi v1.6.0 openai-completions-tool-choice.test.ts:1208-1213.
func TestSourceV160CompletionsOpenRouterKimiReplayMetadata(t *testing.T) {
	model := requireModel(t, goai.ProviderOpenRouter, "moonshotai/kimi-k2.6")
	if compat := model.CompletionsCompat; compat == nil || compat.RequiresReasoningContentOnAssistantMessages == nil || !*compat.RequiresReasoningContentOnAssistantMessages {
		t.Fatalf("reasoning replay must be explicitly required, got %+v", compat)
	}
}

func TestXiaomiModelsOmitRetiredMimoV2Flash(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, provider := range []goai.Provider{goai.ProviderXiaomi, goai.ProviderXiaomiTokenPlanCN, goai.ProviderXiaomiTokenPlanAMS, goai.ProviderXiaomiTokenPlanSGP} {
		if model := goai.GetModel(provider, "mimo-v2-flash"); model != nil {
			t.Fatalf("retired mimo-v2-flash should be omitted from %s", provider)
		}
	}
}

// Source: cache-retention.test.ts. A documented TTL is not a warming policy.
// Source: pi v1.6.0 packages/ai/test/cache-retention.test.ts:243-255.
func TestSourceV160OpenAIModelsDoNotInferCacheWarming(t *testing.T) {
	for _, id := range []string{"gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-6-astra", "gpt-6-luna", "gpt-6-sol", "gpt-6.1-terra", "gpt-6.2-astra", "gpt-6.2-sol"} {
		t.Run(id, func(t *testing.T) {
			model := requireModel(t, goai.ProviderOpenAI, id)
			if model.PromptCache != nil {
				t.Errorf("unexpected cache warming policy: %+v", model.PromptCache)
			}
		})
	}
}
