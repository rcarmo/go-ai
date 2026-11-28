package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

// Source locator: packages/ai/test/azure-models.test.ts, "keeps OpenAI's long-context price tier on Azure OpenAI models".
func TestAzureModelsUpstreamPreservesOpenAILongContextTier(t *testing.T) {
	goai.RegisterBuiltinModels()
	model := goai.GetModel(goai.ProviderAzureOpenAI, "gpt-6-sol")
	if model == nil {
		t.Fatal("missing Azure gpt-6-sol")
	}
	if model.Provider != goai.ProviderAzureOpenAI {
		t.Fatalf("provider = %q", model.Provider)
	}
	if model.Api != goai.ApiAzureOpenAIResponses {
		t.Fatalf("api = %q", model.Api)
	}
	if model.ContextWindow != 1_050_000 {
		t.Fatalf("contextWindow = %d", model.ContextWindow)
	}
	wantCost := goai.ModelCost{
		Input: 2, Output: 10, CacheRead: 0.2, CacheWrite: 2.5,
		Tiers: []goai.ModelCostTier{{InputTokensAbove: 272000, Input: 4, Output: 15, CacheRead: 0.4, CacheWrite: 5}},
	}
	if !reflect.DeepEqual(model.Cost, wantCost) {
		t.Fatalf("cost = %#v, want %#v", model.Cost, wantCost)
	}
}

// Source locator: azure-openai-completions.test.ts, "azure api map".
func TestAzureAPIUpstreamRoutesCompletionsAndResponsesModels(t *testing.T) {
	goai.RegisterBuiltinModels()
	responses := goai.GetModel(goai.ProviderAzureOpenAI, "gpt-4o-mini")
	completions := goai.GetModel(goai.ProviderAzureOpenAI, "deepseek-v4-pro")
	if responses == nil || responses.Api != goai.ApiAzureOpenAIResponses {
		t.Fatalf("Responses route = %#v", responses)
	}
	if completions == nil || completions.Api != goai.ApiOpenAICompletions {
		t.Fatalf("Completions route = %#v", completions)
	}
	if goai.GetApiProvider(responses.Api) == nil || goai.GetApiProvider(completions.Api) == nil {
		t.Fatal("Azure API providers are not registered")
	}
}
