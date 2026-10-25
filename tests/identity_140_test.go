package goai_test

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	imageprovider "github.com/rcarmo/go-ai/images/openrouter"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentity140HTTPProviderAndImageHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, modelUA, requestUA string
		suppress                 bool
		wantUA                   string
	}{
		{name: "identity", wantUA: "identity-agent"},
		{name: "model", modelUA: "model-agent", wantUA: "model-agent"},
		{name: "request", modelUA: "model-agent", requestUA: "request-agent", wantUA: "request-agent"},
		{name: "suppression", suppress: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, image := range []bool{false, true} {
				var got http.Header
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					got = r.Header.Clone()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":{"message":"fixture"}}`))
				}))
				custom := "identity-agent"
				identity := &goai.ClientIdentity{UserAgent: &goai.UserAgentMode{Custom: &custom}, Attribution: &goai.ClientAttribution{Name: "app", URL: "https://example.com"}}
				mh, oh := map[string]string{}, map[string]string{}
				if tc.modelUA != "" {
					mh["User-Agent"] = tc.modelUA
				}
				if tc.requestUA != "" {
					oh["user-agent"] = tc.requestUA
				}
				var suppress []string
				if tc.suppress {
					suppress = []string{"User-Agent", "X-OpenRouter-Title"}
				}
				if image {
					_, _ = imageprovider.GenerateImagesOpenRouter(&images.ImagesModel{ID: "image", Provider: images.ImagesProviderOpenRouter, BaseURL: server.URL, Headers: mh}, images.ImagesContext{}, &images.ImagesOptions{APIKey: "test", Identity: identity, Headers: oh, SuppressHeaders: suppress})
				} else {
					for range goai.Stream(context.Background(), &goai.Model{ID: "chat", Api: goai.ApiOpenAICompletions, Provider: goai.ProviderOpenRouter, BaseURL: server.URL, Headers: mh}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "test", Identity: identity, Headers: oh, SuppressHeaders: suppress, RetryConfig: func() *goai.RetryConfig { c := goai.NoRetryConfig(); return &c }()}) {
					}
				}
				server.Close()
				if got == nil {
					t.Fatalf("image=%v: no HTTP request", image)
				}
				// net/http adds its own default if User-Agent is suppressed; verify only
				// that suppression removed the identity value and attribution title.
				if !tc.suppress && got.Get("User-Agent") != tc.wantUA {
					t.Fatalf("image=%v headers=%v", image, got)
				}
				if got.Get("HTTP-Referer") != "https://example.com" {
					t.Fatal(got)
				}
				if tc.suppress {
					if got.Get("User-Agent") == custom || got.Get("X-OpenRouter-Title") != "" {
						t.Fatal(got)
					}
				} else if got.Get("X-OpenRouter-Title") != "app" {
					t.Fatal(got)
				}
			}
		})
	}
}
