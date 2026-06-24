package openairesponses

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAzureOpenAIBaseURLNormalizesCognitiveServicesRootEndpoints(t *testing.T) {
	assertAzureBaseURL(t, "https://marc-quicktests-resource.cognitiveservices.azure.com", "https://marc-quicktests-resource.cognitiveservices.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLNormalizesAzureOpenAIRootEndpoints(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.openai.azure.com", "https://my-resource.openai.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLNormalizesOpenAIPathToV1(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.cognitiveservices.azure.com/openai", "https://my-resource.cognitiveservices.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLPreservesOpenAIV1Endpoints(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.cognitiveservices.azure.com/openai/v1", "https://my-resource.cognitiveservices.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLPreservesExplicitNonAzureProxyPaths(t *testing.T) {
	assertAzureBaseURL(t, "https://my-proxy.example.com/v1", "https://my-proxy.example.com/v1")
}

func TestAzureOpenAIBaseURLStripsQueryParamsWhenNormalizingAzureHostURLs(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.openai.azure.com/openai?api-version=2024-12-01", "https://my-resource.openai.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLPreservesQueryParamsOnNonAzureProxyURLs(t *testing.T) {
	assertAzureBaseURL(t, "https://my-proxy.example.com/v1?custom=true", "https://my-proxy.example.com/v1?custom=true")
}

func TestAzureOpenAIBaseURLThrowsOnInvalidURLs(t *testing.T) {
	_, err := normalizeAzureBaseURL("not-a-url")
	if err == nil || !strings.Contains(err.Error(), "invalid Azure OpenAI base URL") {
		t.Fatalf("expected invalid Azure OpenAI base URL error, got %v", err)
	}
}

func TestAzureOpenAIBaseURLClampsPromptCacheKeyToOpenAI64CharacterLimit(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{SessionID: strings.Repeat("x", 67)})
	if got, want := req.PromptCacheKey, strings.Repeat("x", 64); got != want {
		t.Fatalf("prompt_cache_key = %q, want %q", got, want)
	}
}

func TestAzureOpenAIBaseURLDisablesServerSideResponseStorage(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, nil)
	if req.Store {
		t.Fatal("store = true, want false")
	}
}

func TestAzureOpenAIBaseURLBuildsCorrectDefaultURLFromResourceName(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	baseURL, _, _, err := resolveAzureResponsesConfig(model, &goai.StreamOptions{Env: goai.ProviderEnv{"AZURE_OPENAI_RESOURCE_NAME": "my-resource"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSuffix(baseURL, "/deployments/gpt-4o-mini"), "https://my-resource.openai.azure.com/openai/v1"; got != want {
		t.Fatalf("baseURL = %q, want %q", got, want)
	}
}

func assertAzureBaseURL(t *testing.T, input, want string) {
	t.Helper()
	got, err := normalizeAzureBaseURL(input)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("normalizeAzureBaseURL(%q) = %q, want %q", input, got, want)
	}
}
