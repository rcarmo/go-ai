package google

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGoogleVertexAPIKeyResolutionFallsBackToADCWhenOptionsAPIKeyIsPlaceholder(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex}
	url, err := buildStreamURL(model, "<authenticated>", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "key=") {
		t.Fatalf("url should not include api key: %s", url)
	}
	if !strings.Contains(url, "/v1/projects/test-project/locations/us-central1/") {
		t.Fatalf("url=%s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionFallsBackToADCWhenOptionsAPIKeyIsGCPCredentialsMarker(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex}
	url, err := buildStreamURL(model, "gcp-vertex-credentials", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "key=") {
		t.Fatalf("url should not include api key marker: %s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionFallsBackToADCWhenEnvAPIKeyIsPlaceholder(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex}
	url, err := buildStreamURL(model, "<authenticated>", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "key=") {
		t.Fatalf("url should not include api key: %s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionStillUsesAPIKeyClientForRealAPIKeys(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex}
	url, err := buildStreamURL(model, "AIzaSyExampleRealisticLookingApiKey123456", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "key=AIzaSyExampleRealisticLookingApiKey123456") {
		t.Fatalf("url missing real api key: %s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionDoesNotForwardGeneratedVertexBaseURLPlaceholders(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex, BaseURL: "https://{location}-aiplatform.googleapis.com"}
	url, err := buildStreamURL(model, "", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "{location}") {
		t.Fatalf("placeholder not resolved: %s", url)
	}
	if !strings.HasPrefix(url, "https://us-central1-aiplatform.googleapis.com/v1/projects/") {
		t.Fatalf("url=%s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionForwardsCustomBaseURLToADCClient(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex, BaseURL: "https://proxy.example.com"}
	url, err := buildStreamURL(model, "", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "https://proxy.example.com/v1/projects/test-project/locations/us-central1/") {
		t.Fatalf("url=%s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionForwardsCustomBaseURLToAPIKeyClient(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex, BaseURL: "https://proxy.example.com"}
	url, err := buildStreamURL(model, "AIzaSyExampleRealisticLookingApiKey123456", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "https://proxy.example.com/v1/projects/test-project/locations/us-central1/") || !strings.Contains(url, "key=AIzaSyExampleRealisticLookingApiKey123456") {
		t.Fatalf("url=%s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionDoesNotAppendAPIVersionWhenCustomBaseURLAlreadyIncludesOne(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex, BaseURL: "https://proxy.example.com/v1/projects/test-project/locations/global"}
	url, err := buildStreamURL(model, "", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := "https://proxy.example.com/v1/projects/test-project/locations/global/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse"; url != got {
		t.Fatalf("url=%s, want %s", url, got)
	}
}
