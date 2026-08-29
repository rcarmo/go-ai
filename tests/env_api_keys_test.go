package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestEnvAPIKeysDoesNotTreatGenericGitHubTokensAsGitHubCopilotCredentials(t *testing.T) {
	t.Setenv("COPILOT_GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh-token")
	t.Setenv("GITHUB_TOKEN", "github-token")
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderGitHubCopilot, goai.ProviderEnv{}); got != "" {
		t.Fatalf("github-copilot env key = %q, want empty", got)
	}
}

func TestEnvAPIKeysResolvesGitHubCopilotCredentialsFromCopilotGitHubToken(t *testing.T) {
	env := goai.ProviderEnv{"COPILOT_GITHUB_TOKEN": "copilot-token", "GH_TOKEN": "gh-token", "GITHUB_TOKEN": "github-token"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderGitHubCopilot, env); got != "copilot-token" {
		t.Fatalf("github-copilot env key = %q, want copilot-token", got)
	}
}

func TestEnvAPIKeysResolvesZAIChinaCodingPlanCredentials(t *testing.T) {
	env := goai.ProviderEnv{"ZAI_CODING_CN_API_KEY": "zai-coding-cn-token"}
	if got := goai.GetEnvAPIKeyWithEnv(goai.ProviderZAICodingCN, env); got != "zai-coding-cn-token" {
		t.Fatalf("zai-coding-cn env key = %q, want zai-coding-cn-token", got)
	}
}
