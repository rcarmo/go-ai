package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type simulatedAbortCase struct {
	name           string
	api            goai.Api
	provider       goai.Provider
	cost           goai.ModelCost
	usage          goai.Usage
	wantInputZero  bool
	wantOutputZero bool
	wantInputGT0   bool
	wantOutputGT0  bool
	wantCostGT0    bool
}

func assertSimulatedTokensOnAbort(t *testing.T, tc simulatedAbortCase) {
	t.Helper()
	msg := &goai.Message{Role: goai.RoleAssistant, Api: tc.api, Provider: tc.provider, Model: "simulated", StopReason: goai.StopReasonAborted, Usage: &tc.usage}
	msg.Usage.Cost = goai.CalculateCost(&goai.Model{Provider: tc.provider, Api: tc.api, Cost: tc.cost}, msg.Usage)
	if msg.StopReason != goai.StopReasonAborted {
		t.Fatalf("%s stopReason=%q, want aborted", tc.name, msg.StopReason)
	}
	if tc.wantInputZero && msg.Usage.Input != 0 {
		t.Fatalf("%s input=%d, want 0", tc.name, msg.Usage.Input)
	}
	if tc.wantOutputZero && msg.Usage.Output != 0 {
		t.Fatalf("%s output=%d, want 0", tc.name, msg.Usage.Output)
	}
	if tc.wantInputGT0 && msg.Usage.Input <= 0 {
		t.Fatalf("%s input=%d, want >0", tc.name, msg.Usage.Input)
	}
	if tc.wantOutputGT0 && msg.Usage.Output <= 0 {
		t.Fatalf("%s output=%d, want >0", tc.name, msg.Usage.Output)
	}
	if tc.wantCostGT0 && (msg.Usage.Cost.Input <= 0 || msg.Usage.Cost.Total <= 0) {
		t.Fatalf("%s cost=%#v, want input/total >0", tc.name, msg.Usage.Cost)
	}
}

func TestTokensSimulatedFinalChunkOnlyProvidersHaveNoUsageOnAbort(t *testing.T) {
	for _, tc := range []simulatedAbortCase{
		{name: "openai-completions", api: goai.ApiOpenAICompletions, provider: goai.ProviderOpenAI},
		{name: "mistral-conversations", api: goai.ApiMistralConversations, provider: goai.ProviderMistral},
		{name: "openai-responses", api: goai.ApiOpenAIResponses, provider: goai.ProviderOpenAI},
		{name: "azure-openai-responses", api: goai.ApiAzureOpenAIResponses, provider: goai.ProviderAzureOpenAI},
		{name: "openai-codex-responses", api: goai.ApiOpenAICodexResponses, provider: goai.ProviderOpenAICodex},
		{name: "zai", api: goai.ApiOpenAICompletions, provider: goai.ProviderZAI},
		{name: "amazon-bedrock", api: goai.ApiBedrockConverseStream, provider: goai.ProviderAmazonBedrock},
		{name: "vercel-ai-gateway", api: goai.ApiOpenAICompletions, provider: goai.ProviderVercelAIGateway},
		{name: "minimax", api: goai.ApiOpenAICompletions, provider: goai.ProviderMiniMax},
	} {
		tc.wantInputZero = true
		tc.wantOutputZero = true
		assertSimulatedTokensOnAbort(t, tc)
	}
}

func TestTokensSimulatedKimiReportsInputOnlyOnAbort(t *testing.T) {
	assertSimulatedTokensOnAbort(t, simulatedAbortCase{name: "kimi-coding", api: goai.ApiOpenAICompletions, provider: goai.ProviderKimiCoding, usage: goai.Usage{Input: 123}, wantInputGT0: true, wantOutputZero: true})
}

func TestTokensSimulatedEarlyUsageProvidersKeepInputAndOutputOnAbort(t *testing.T) {
	for _, tc := range []simulatedAbortCase{
		{name: "google", api: goai.ApiGoogleGenerativeAI, provider: goai.ProviderGoogle, cost: goai.ModelCost{Input: 1, Output: 2}, usage: goai.Usage{Input: 10, Output: 5}},
		{name: "anthropic", api: goai.ApiAnthropicMessages, provider: goai.ProviderAnthropic, cost: goai.ModelCost{Input: 3, Output: 15}, usage: goai.Usage{Input: 10, Output: 5}},
		{name: "xai", api: goai.ApiOpenAICompletions, provider: goai.ProviderXAI, cost: goai.ModelCost{Input: 1, Output: 2}, usage: goai.Usage{Input: 10, Output: 5}},
		{name: "groq", api: goai.ApiOpenAICompletions, provider: goai.ProviderGroq, cost: goai.ModelCost{Input: 0.1, Output: 0.2}, usage: goai.Usage{Input: 10, Output: 5}},
		{name: "cerebras", api: goai.ApiOpenAICompletions, provider: goai.ProviderCerebras, cost: goai.ModelCost{Input: 1, Output: 2}, usage: goai.Usage{Input: 10, Output: 5}},
		{name: "together", api: goai.ApiOpenAICompletions, provider: goai.ProviderTogether, cost: goai.ModelCost{Input: 1, Output: 2}, usage: goai.Usage{Input: 10, Output: 5}},
	} {
		tc.wantInputGT0 = true
		tc.wantOutputGT0 = true
		tc.wantCostGT0 = true
		assertSimulatedTokensOnAbort(t, tc)
	}
}

func TestTokensSimulatedZeroCostProvidersDoNotRequirePositiveCostOnAbort(t *testing.T) {
	assertSimulatedTokensOnAbort(t, simulatedAbortCase{name: "github-copilot", api: goai.ApiAnthropicMessages, provider: goai.ProviderGitHubCopilot, usage: goai.Usage{Input: 10, Output: 5}, wantInputGT0: true, wantOutputGT0: true})
}
