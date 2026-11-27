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

// Source: pi v1.6.0 packages/ai/test/fireworks-models.test.ts:182-187.
func TestFireworksProviderEnvKeyCompatibilityRetained(t *testing.T) {
	t.Setenv("FIREWORKS_API_KEY", "test-fireworks-key")
	if got := goai.FindEnvKeys(goai.ProviderFireworks, nil); !reflect.DeepEqual(got, []string{"FIREWORKS_API_KEY"}) {
		t.Fatalf("Fireworks environment names = %q, want [FIREWORKS_API_KEY]", got)
	}
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
// The retired inputs come from the same published catalog; the live MiniMax
// successor and captured requests are supplemental controls.
func TestTogetherModelsReasoningControlsFromTogetherAPISurface(t *testing.T) {
	const archive = "together-reasoning-v160.json"
	gptOss := loadHistoricalCompletionsModel(t, archive, 3, goai.ProviderTogether, "openai/gpt-oss-120b")
	low, medium, high := "low", "medium", "high"
	wantGPTOSS := map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil, goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): &low, goai.ModelThinkingLevel(goai.ThinkingMedium): &medium, goai.ModelThinkingLevel(goai.ThinkingHigh): &high, goai.ModelThinkingLevel(goai.ThinkingMax): nil, goai.ModelThinkingLevel(goai.ThinkingXHigh): nil}
	if !reflect.DeepEqual(gptOss.ThinkingLevelMap, wantGPTOSS) {
		t.Fatalf("gpt-oss thinkingLevelMap=%#v, want %#v", gptOss.ThinkingLevelMap, wantGPTOSS)
	}
	if gptOss.CompletionsCompat == nil || ptrValue(gptOss.CompletionsCompat.SupportsReasoningEffort) != true || gptOss.CompletionsCompat.ThinkingFormat != "openai" {
		t.Fatalf("gpt-oss compat=%#v, want supportsReasoningEffort true and thinkingFormat openai", gptOss.CompletionsCompat)
	}

	deepSeekV4 := loadHistoricalCompletionsModel(t, archive, 3, goai.ProviderTogether, "deepseek-ai/DeepSeek-V4-Pro-0813")
	wantDeepSeek := map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil, goai.ModelThinkingLevel(goai.ThinkingHigh): &high, goai.ModelThinkingLevel(goai.ThinkingXHigh): nil}
	if !reflect.DeepEqual(deepSeekV4.ThinkingLevelMap, wantDeepSeek) {
		t.Fatalf("DeepSeek-V4 thinkingLevelMap=%#v, want %#v", deepSeekV4.ThinkingLevelMap, wantDeepSeek)
	}
	if deepSeekV4.CompletionsCompat == nil || ptrValue(deepSeekV4.CompletionsCompat.SupportsReasoningEffort) != true || deepSeekV4.CompletionsCompat.ThinkingFormat != "together" {
		t.Fatalf("DeepSeek-V4 compat=%#v, want supportsReasoningEffort true and thinkingFormat together", deepSeekV4.CompletionsCompat)
	}

	minimax := loadHistoricalCompletionsModel(t, archive, 3, goai.ProviderTogether, "MiniMaxAI/MiniMax-M3")
	liveMiniMax := *requireModel(t, goai.ProviderTogether, "MiniMaxAI/MiniMax-M3.1-Flash")
	wantMiniMax := map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil}
	for _, model := range []goai.Model{minimax, liveMiniMax} {
		if !reflect.DeepEqual(model.ThinkingLevelMap, wantMiniMax) {
			t.Fatalf("%s thinkingLevelMap=%#v, want %#v", model.ID, model.ThinkingLevelMap, wantMiniMax)
		}
		compat := model.CompletionsCompat
		if compat == nil || compat.ThinkingFormat != "together" || compat.SupportsReasoningEffort == nil || *compat.SupportsReasoningEffort {
			t.Fatalf("%s compat=%#v, want thinkingFormat together and explicit supportsReasoningEffort=false", model.ID, compat)
		}
	}
	for _, retired := range []goai.Model{gptOss, deepSeekV4, minimax} {
		if got := goai.GetModel(retired.Provider, retired.ID); got != nil {
			t.Errorf("retired model %s is still in the live Together catalog", retired.ID)
		}
	}

	for _, tc := range []struct {
		name              string
		model             goai.Model
		reasoning, effort string
	}{
		{"historical OpenAI effort", gptOss, "", `"high"`},
		{"historical Together effort and toggle", deepSeekV4, `{"enabled":true}`, `"high"`},
		{"historical Together toggle only", minimax, `{"enabled":true}`, ""},
		{"live Together toggle only", liveMiniMax, `{"enabled":true}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := captureCompletionsPayload(t, tc.model, goai.ThinkingHigh)
			assertCompletionsPayloadField(t, payload, "model", `"`+tc.model.ID+`"`)
			assertCompletionsPayloadField(t, payload, "reasoning", tc.reasoning)
			assertCompletionsPayloadField(t, payload, "reasoning_effort", tc.effort)
		})
	}
}

// Source: pi v1.6.0 packages/ai/test/together-models.test.ts:74-79.
func TestTogetherModelsResolvesTogetherAPIKeyFromEnvironment(t *testing.T) {
	t.Setenv("TOGETHER_API_KEY", "test-together-key")
	if got := goai.FindEnvKeys(goai.ProviderTogether, nil); !reflect.DeepEqual(got, []string{"TOGETHER_API_KEY"}) {
		t.Fatalf("Together environment names = %q, want [TOGETHER_API_KEY]", got)
	}
	if got := goai.GetEnvAPIKey(goai.ProviderTogether); got != "test-together-key" {
		t.Fatalf("together env key=%q, want test-together-key", got)
	}
}

func TestFindEnvKeysReportsNamesWithoutChangingCredentialResolution(t *testing.T) {
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "test-bearer")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "test-oauth")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	env := goai.ProviderEnv{"ANTHROPIC_OAUTH_TOKEN": ""}
	want := []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_API_KEY"}
	if got := goai.FindEnvKeys(goai.ProviderAnthropic, env); !reflect.DeepEqual(got, want) {
		t.Fatalf("Anthropic environment names = %q, want %q", got, want)
	}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderAnthropic, env); got != "test-key" {
		t.Fatal("existing scoped-empty override or bearer/API key separation changed")
	}
	if got := goai.FindEnvKeys(goai.ProviderAmazonBedrock, goai.ProviderEnv{"AWS_PROFILE": "test-profile"}); got != nil {
		t.Errorf("Bedrock discovered an ambient credential: %q", got)
	}
	if got := goai.FindEnvKeys("custom-provider", goai.ProviderEnv{"CUSTOM_PROVIDER_API_KEY": "test"}); got != nil {
		t.Errorf("unknown provider names = %q, want nil", got)
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
