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

func TestFireworksModelsRegistersDefaultKimiK26ViaAnthropicCompatibleMessagesAPI(t *testing.T) {
	model := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/kimi-k2p6")
	if model.Api != goai.ApiAnthropicMessages {
		t.Fatalf("api=%q, want anthropic-messages", model.Api)
	}
	if model.Provider != goai.ProviderFireworks {
		t.Fatalf("provider=%q, want fireworks", model.Provider)
	}
	if model.BaseURL != "https://api.fireworks.ai/inference" {
		t.Fatalf("baseUrl=%q, want https://api.fireworks.ai/inference", model.BaseURL)
	}
	if !model.Reasoning {
		t.Fatalf("reasoning=false, want true")
	}
	if !reflect.DeepEqual(model.Input, []string{"text", "image"}) {
		t.Fatalf("input=%#v, want [text image]", model.Input)
	}
	if model.ContextWindow != 262000 || model.MaxTokens != 262000 {
		t.Fatalf("contextWindow/maxTokens=%d/%d, want 262000/262000", model.ContextWindow, model.MaxTokens)
	}
	wantCost := goai.ModelCost{Input: 0.95, Output: 4, CacheRead: 0.16, CacheWrite: 0}
	if !reflect.DeepEqual(model.Cost, wantCost) {
		t.Fatalf("cost=%#v, want %#v", model.Cost, wantCost)
	}
}

func TestFireworksModelsRegistersFirePassTurboRouterModel(t *testing.T) {
	goai.RegisterBuiltinModels()
	var model *goai.Model
	for _, candidate := range goai.ListModels(goai.ProviderFireworks) {
		if candidate.ID == "accounts/fireworks/routers/kimi-k2p6-turbo" {
			model = candidate
			break
		}
	}
	if model == nil {
		t.Fatalf("Fire Pass turbo router model not found")
	}
	if model.Api != goai.ApiAnthropicMessages {
		t.Fatalf("api=%q, want anthropic-messages", model.Api)
	}
	if model.BaseURL != "https://api.fireworks.ai/inference" {
		t.Fatalf("baseUrl=%q, want https://api.fireworks.ai/inference", model.BaseURL)
	}
	if !reflect.DeepEqual(model.Input, []string{"text", "image"}) {
		t.Fatalf("input=%#v, want [text image]", model.Input)
	}
}

func TestFireworksModelsResolvesFireworksAPIKeyFromEnvironment(t *testing.T) {
	env := goai.ProviderEnv{"FIREWORKS_API_KEY": "test-fireworks-key"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderFireworks, env); got != "test-fireworks-key" {
		t.Fatalf("fireworks env key=%q, want test-fireworks-key", got)
	}
}

func TestFireworksModelsSetsFireworksSpecificCompat(t *testing.T) {
	model := requireModel(t, goai.ProviderFireworks, "accounts/fireworks/models/kimi-k2p6")
	if model.AnthropicCompat == nil {
		t.Fatalf("AnthropicCompat is nil")
	}
	if got := ptrValue(model.AnthropicCompat.SupportsEagerToolInputStreaming); got != false {
		t.Fatalf("supportsEagerToolInputStreaming=%v, want false", got)
	}
	if got := ptrValue(model.AnthropicCompat.SupportsLongCacheRetention); got != false {
		t.Fatalf("supportsLongCacheRetention=%v, want false", got)
	}
}

func TestTogetherModelsRegistersDefaultKimiK26ViaOpenAICompatibleChatCompletionsAPI(t *testing.T) {
	model := requireModel(t, goai.ProviderTogether, "moonshotai/Kimi-K2.6")
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
	if model.ContextWindow != 262144 || model.MaxTokens != 131000 {
		t.Fatalf("contextWindow/maxTokens=%d/%d, want 262144/131000", model.ContextWindow, model.MaxTokens)
	}
	wantCost := goai.ModelCost{Input: 1.2, Output: 4.5, CacheRead: 0.2, CacheWrite: 0}
	if !reflect.DeepEqual(model.Cost, wantCost) {
		t.Fatalf("cost=%#v, want %#v", model.Cost, wantCost)
	}
	compat := model.CompletionsCompat
	if compat == nil {
		t.Fatalf("CompletionsCompat is nil")
	}
	if ptrValue(compat.SupportsStore) != false || ptrValue(compat.SupportsDeveloperRole) != false || ptrValue(compat.SupportsReasoningEffort) != false || compat.MaxTokensField != "max_tokens" || compat.ThinkingFormat != "together" || ptrValue(compat.SupportsStrictMode) != false || ptrValue(compat.SupportsLongCacheRetention) != false {
		t.Fatalf("compat=%#v, want upstream Together Kimi compat", compat)
	}
}

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

	deepSeekV4 := requireModel(t, goai.ProviderTogether, "deepseek-ai/DeepSeek-V4-Pro")
	wantDeepSeek := map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil, goai.ModelThinkingLevel(goai.ThinkingHigh): &high, goai.ModelThinkingLevel(goai.ThinkingXHigh): nil}
	if !reflect.DeepEqual(deepSeekV4.ThinkingLevelMap, wantDeepSeek) {
		t.Fatalf("DeepSeek-V4 thinkingLevelMap=%#v, want %#v", deepSeekV4.ThinkingLevelMap, wantDeepSeek)
	}
	if deepSeekV4.CompletionsCompat == nil || ptrValue(deepSeekV4.CompletionsCompat.SupportsReasoningEffort) != true || deepSeekV4.CompletionsCompat.ThinkingFormat != "together" {
		t.Fatalf("DeepSeek-V4 compat=%#v, want supportsReasoningEffort true and thinkingFormat together", deepSeekV4.CompletionsCompat)
	}

	minimax := requireModel(t, goai.ProviderTogether, "MiniMaxAI/MiniMax-M2.7")
	wantMiniMax := map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: nil, goai.ModelThinkingLevel(goai.ThinkingMinimal): nil, goai.ModelThinkingLevel(goai.ThinkingLow): nil, goai.ModelThinkingLevel(goai.ThinkingMedium): nil}
	if !reflect.DeepEqual(minimax.ThinkingLevelMap, wantMiniMax) {
		t.Fatalf("MiniMax thinkingLevelMap=%#v, want %#v", minimax.ThinkingLevelMap, wantMiniMax)
	}
	if minimax.CompletionsCompat == nil || minimax.CompletionsCompat.ThinkingFormat != "" || ptrValue(minimax.CompletionsCompat.SupportsReasoningEffort) != false {
		t.Fatalf("MiniMax compat=%#v, want no thinkingFormat and supportsReasoningEffort false", minimax.CompletionsCompat)
	}
}

func TestTogetherModelsResolvesTogetherAPIKeyFromEnvironment(t *testing.T) {
	env := goai.ProviderEnv{"TOGETHER_API_KEY": "test-together-key"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderTogether, env); got != "test-together-key" {
		t.Fatalf("together env key=%q, want test-together-key", got)
	}
}

func TestXiaomiModelsKeepsMimoV25OnAPIBillingAndTokenPlanProviders(t *testing.T) {
	goai.RegisterBuiltinModels()
	if model := goai.GetModel(goai.ProviderXiaomi, "mimo-v2.5"); model == nil {
		t.Fatalf("mimo-v2.5 should be present for xiaomi")
	}
	for _, provider := range []goai.Provider{goai.ProviderXiaomiTokenPlanCN, goai.ProviderXiaomiTokenPlanAMS, goai.ProviderXiaomiTokenPlanSGP} {
		if model := goai.GetModel(provider, "mimo-v2.5"); model == nil {
			t.Fatalf("mimo-v2.5 should be present for %s", provider)
		}
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
