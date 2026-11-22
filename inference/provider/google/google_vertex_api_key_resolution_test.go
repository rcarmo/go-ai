package google

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	if url != "https://aiplatform.googleapis.com/v1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse" {
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
	// Both ADC and key SDK clients select custom collection scope upstream.
	if url != "https://proxy.example.com/v1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse" {
		t.Fatalf("url=%s", url)
	}
}

func TestGoogleVertexAPIKeyResolutionForwardsCustomBaseURLToAPIKeyClient(t *testing.T) {
	model := &goai.Model{ID: "gemini-3-flash-preview", Provider: goai.ProviderGoogleVertex, Api: goai.ApiGoogleVertex, BaseURL: "https://proxy.example.com"}
	url, err := buildStreamURL(model, "AIzaSyExampleRealisticLookingApiKey123456", &goai.StreamOptions{Project: "test-project", Location: "us-central1"})
	if err != nil {
		t.Fatal(err)
	}
	// API-key clients omit the project/location constructor fields upstream.
	if url != "https://proxy.example.com/v1/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse" {
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

// Translate the pinned SDK-constructor checks through the actual Vertex HTTP
// request. The fake transport supplies an offline ADC exchange where the
// original mock bypasses its client internals. The final three cases are
// supplemental public/locked-SDK probes.
func TestSourceV160GoogleVertexAPIKeyTransport(t *testing.T) {
	goai.RegisterBuiltinModels()
	const key = "AIzaSyExampleRealisticLookingApiKey123456"
	const proxy = "https://proxy.example.com"
	const projectBase = proxy + "/v1/projects/test-project/locations/global"
	const expressBase = "https://aiplatform.googleapis.com/v1"
	const regionalBase = "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1"
	for _, tc := range []struct {
		name, optionKey, envKey, modelBase, wantBase, headerKey, wantKey string
		adc, ambient, customAgent                                        bool
	}{
		{name: "option-placeholder", optionKey: "<authenticated>", wantBase: regionalBase, adc: true},
		{name: "option-vertex-marker", optionKey: "gcp-vertex-credentials", wantBase: regionalBase, adc: true},
		{name: "environment-placeholder", envKey: "<authenticated>", wantBase: regionalBase, adc: true},
		{name: "real-key", optionKey: key, wantBase: expressBase, wantKey: key},
		{name: "generated-placeholder", wantBase: regionalBase, adc: true, ambient: true},
		{name: "override-user-agent", wantBase: regionalBase, adc: true, ambient: true, customAgent: true},
		{name: "custom-adc-base", modelBase: proxy, wantBase: proxy + "/v1", adc: true},
		{name: "custom-key-base", optionKey: key, modelBase: proxy, wantBase: proxy + "/v1", wantKey: key},
		{name: "versioned-extension", modelBase: projectBase, wantBase: projectBase, adc: true},
		{name: "supplemental-blank-option", optionKey: "\u00a0 \t", envKey: key, wantBase: expressBase, wantKey: key},
		{name: "supplemental-explicit-marker", optionKey: "<authenticated>", envKey: key, wantBase: regionalBase, adc: true},
		{name: "supplemental-key-header-override", optionKey: key, headerKey: "explicit-fake-key", wantKey: "explicit-fake-key", wantBase: expressBase},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOOGLE_CLOUD_API_KEY", tc.envKey)
			catalog := goai.GetModel(goai.ProviderGoogleVertex, "gemini-3-flash-preview")
			if catalog == nil {
				t.Fatal("missing pinned Vertex model")
			}
			model := *catalog
			if tc.modelBase != "" {
				model.BaseURL = tc.modelBase
			}
			var requests, tokens, unexpected atomic.Int32
			savedTokenClient := newVertexTokenClient
			t.Cleanup(func() { newVertexTokenClient = savedTokenClient })
			newVertexTokenClient = func(*goai.StreamOptions) *http.Client {
				return &http.Client{Transport: vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
					tokens.Add(1)
					defer r.Body.Close()
					if !tc.adc || r.Method != http.MethodPost || r.URL.String() != "https://oauth2.googleapis.com/token" {
						return nil, fmt.Errorf("unexpected ADC token exchange")
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"offline-adc-token","token_type":"Bearer","expires_in":3600}`))}, nil
				})}
			}
			wire := vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
				requests.Add(1)
				defer r.Body.Close()
				wantURL := tc.wantBase + "/publishers/google/models/gemini-3-flash-preview:streamGenerateContent?alt=sse"
				if r.Method != http.MethodPost || r.URL.String() != wantURL {
					// Do not echo actual key material even in a synthetic fixture.
					t.Errorf("wrong method/Vertex URL or unexpected query key; want POST %s", wantURL)
				}
				gotKey := r.Header.Values("x-goog-api-key")
				if (tc.wantKey == "" && len(gotKey) != 0) || (tc.wantKey != "" && (len(gotKey) != 1 || gotKey[0] != tc.wantKey)) {
					t.Error("wrong API key header presence or value")
				}
				wantAgent := goai.FormatUserAgent(nil)
				if tc.customAgent {
					wantAgent = "custom-agent"
				}
				wantAuth := ""
				if tc.adc {
					wantAuth = "Bearer offline-adc-token"
				}
				if r.Header.Get("User-Agent") != wantAgent || r.Header.Get("Authorization") != wantAuth {
					t.Error("unexpected Vertex User-Agent or authorization header")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"responseId\":\"vertex-response-id\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}\n\n"))}, nil
			})
			ambient := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = ambient })
			http.DefaultTransport = vertexTokenWireFunc(func(*http.Request) (*http.Response, error) {
				unexpected.Add(1)
				return nil, fmt.Errorf("request bypassed its Vertex transport")
			})
			opts := &goai.StreamOptions{APIKey: tc.optionKey, HTTPTransport: wire, Env: goai.ProviderEnv{
				"HOME": "", "APPDATA": "", "GOOGLE_APPLICATION_CREDENTIALS": "",
				"GOOGLE_CLOUD_PROJECT": "", "GCLOUD_PROJECT": "", "GOOGLE_CLOUD_LOCATION": "",
			}}
			if tc.ambient {
				http.DefaultTransport, opts.HTTPTransport = wire, nil
			}
			wantTokens := int32(0)
			if tc.adc {
				wantTokens = 1
				opts.Project, opts.Location = "test-project", "us-central1"
				path := filepath.Join(t.TempDir(), "vertex-adc.json")
				if err := os.WriteFile(path, []byte(`{"type":"authorized_user","client_id":"test-client","client_secret":"test-secret","refresh_token":"test-refresh"}`), 0o600); err != nil {
					t.Fatal(err)
				}
				opts.Env["GOOGLE_APPLICATION_CREDENTIALS"] = path
			}
			if tc.customAgent {
				opts.Headers = map[string]string{"User-Agent": "custom-agent"}
			}
			if tc.headerKey != "" {
				opts.Headers = map[string]string{"x-goog-api-key": tc.headerKey}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			result, err := goai.Complete(ctx, &model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts)
			if err != nil || result == nil || result.StopReason != goai.StopReasonStop || result.ResponseID != "vertex-response-id" || goai.ContentText(result.Content) != "ok" {
				t.Errorf("Vertex result error = %v; want successful offline response", err)
			}
			if requests.Load() != 1 || unexpected.Load() != 0 || tokens.Load() != wantTokens {
				t.Errorf("Vertex transport calls = %d, bypasses = %d, token exchanges = %d; want 1, 0, %d", requests.Load(), unexpected.Load(), tokens.Load(), wantTokens)
			}
		})
	}
}
