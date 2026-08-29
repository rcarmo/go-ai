package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func assertSimulatedResponseID(t *testing.T, provider goai.Provider, api goai.Api, id string) {
	t.Helper()
	msg := &goai.Message{Role: goai.RoleAssistant, Provider: provider, Api: api, Model: "simulated", StopReason: goai.StopReasonStop, ResponseID: id, Usage: &goai.Usage{}}
	if msg.StopReason == goai.StopReasonError {
		t.Fatalf("unexpected error response: %#v", msg)
	}
	if msg.ResponseID == "" {
		t.Fatalf("missing responseId for %s/%s", provider, api)
	}
}

func TestResponseIDSimulatedProviderOutputsExposeResponseID(t *testing.T) {
	for _, tc := range []struct {
		provider goai.Provider
		api      goai.Api
		id       string
	}{
		{goai.ProviderGoogle, goai.ApiGoogleGenerativeAI, "google-response-id"},
		{goai.ProviderGoogleVertex, goai.ApiGoogleVertex, "vertex-response-id"},
		{goai.ProviderOpenAI, goai.ApiOpenAICompletions, "chatcmpl-test"},
		{goai.ProviderOpenAI, goai.ApiOpenAIResponses, "resp_test"},
		{goai.ProviderAnthropic, goai.ApiAnthropicMessages, "msg_test"},
		{goai.ProviderAzureOpenAI, goai.ApiAzureOpenAIResponses, "resp_azure"},
		{goai.ProviderMistral, goai.ApiMistralConversations, "mistral-response-id"},
		{goai.ProviderGitHubCopilot, goai.ApiOpenAIResponses, "copilot-openai-response-id"},
		{goai.ProviderGitHubCopilot, goai.ApiAnthropicMessages, "copilot-anthropic-message-id"},
		{goai.ProviderOpenAICodex, goai.ApiOpenAICodexResponses, "codex-response-id"},
	} {
		assertSimulatedResponseID(t, tc.provider, tc.api, tc.id)
	}
}
