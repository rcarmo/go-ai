package githubcopilot

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/oauth"
)

func TestGitHubCopilotSideEffectPackageRegistersOAuthAndTransports(t *testing.T) {
	if oauth.GetProvider("github-copilot") == nil {
		t.Fatal("github-copilot OAuth provider was not registered")
	}
	for _, api := range []goai.Api{goai.ApiOpenAICompletions, goai.ApiOpenAIResponses, goai.ApiAnthropicMessages} {
		if goai.GetApiProvider(api) == nil {
			t.Fatalf("API provider %q was not registered", api)
		}
	}
}

func TestGitHubCopilotRuntimeHelperReturnsFilteredUsableModels(t *testing.T) {
	goai.ClearModels()
	defer goai.RegisterBuiltinModels()

	runtime, err := oauth.RuntimeForGitHubCopilot(&oauth.Credentials{
		Access: "tid=abc;exp=123;proxy-ep=proxy.individual.githubcopilot.com;sku=abc",
		Extra: map[string]interface{}{
			"availableModelIds": []interface{}{"gpt-5.4", "claude-sonnet-4.6"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.APIKey == "" {
		t.Fatal("missing runtime API key")
	}
	var gotGPT, gotClaude bool
	for _, model := range runtime.Models {
		if model.Provider != goai.ProviderGitHubCopilot {
			continue
		}
		switch model.ID {
		case "gpt-5.4":
			gotGPT = true
		case "claude-sonnet-4.6":
			gotClaude = true
		default:
			t.Fatalf("unexpected Copilot model after account filter: %s", model.ID)
		}
		if model.BaseURL != "https://api.individual.githubcopilot.com" {
			t.Fatalf("model %s base URL = %q", model.ID, model.BaseURL)
		}
	}
	if !gotGPT || !gotClaude {
		t.Fatalf("missing expected Copilot models: gpt=%v claude=%v", gotGPT, gotClaude)
	}
}
