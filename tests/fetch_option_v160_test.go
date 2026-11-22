package goai_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	_ "github.com/rcarmo/go-ai/images/openrouter"
	_ "github.com/rcarmo/go-ai/inference/provider/anthropic"
	_ "github.com/rcarmo/go-ai/inference/provider/google"
	_ "github.com/rcarmo/go-ai/inference/provider/mistral"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openaicodex"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
	_ "github.com/rcarmo/go-ai/inference/provider/pimessages"
)

type fetchOptionWire struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (w *fetchOptionWire) RoundTrip(req *http.Request) (*http.Response, error) {
	return w.roundTrip(req)
}

// Source: pi v1.6.0 packages/ai/test/fetch-option.test.ts:63,73,103,125,149,167.
// A request-local RoundTripper is the native streaming fetch seam. Image options
// already take a per-request Client. Loopback catches adapters that construct a
// fresh default transport instead of consulting the installed ambient witness.
func TestSourceV160FetchOption(t *testing.T) {
	for _, tc := range []struct {
		name    string
		apis    []goai.Api
		ambient bool
		image   bool
	}{
		{name: "anthropic", apis: []goai.Api{goai.ApiAnthropicMessages}},
		{name: "openai-adapters", apis: []goai.Api{goai.ApiOpenAICompletions, goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses}},
		{name: "direct-http", apis: []goai.Api{goai.ApiMistralConversations, goai.ApiOpenAICodexResponses, goai.ApiPiMessages}},
		{name: "google-custom", apis: []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex}},
		{name: "google-ambient", apis: []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex}, ambient: true},
		{name: "image-generation", image: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var localCalls, ambientCalls, bypasses atomic.Int32
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				bypasses.Add(1)
				http.Error(w, "request transport was bypassed", http.StatusUnauthorized)
			}))
			defer fallback.Close()
			base := fallback.URL + "/v1"
			const rejection = "upstream rejected request"
			reject := func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodPost || !strings.HasPrefix(req.URL.String(), base+"/") {
					t.Errorf("transport request = %s %s; want POST under %s/", req.Method, req.URL, base)
				}
				return &http.Response{
					Request: req, StatusCode: http.StatusUnauthorized, Status: "401 Unauthorized",
					Header: http.Header{"Content-Type": []string{"application/json"}},
					Body:   io.NopCloser(strings.NewReader(`{"error":{"message":"` + rejection + `"}}`)),
				}, nil
			}
			local := &fetchOptionWire{roundTrip: func(req *http.Request) (*http.Response, error) {
				localCalls.Add(1)
				return reject(req)
			}}
			ambient := &fetchOptionWire{roundTrip: func(req *http.Request) (*http.Response, error) {
				ambientCalls.Add(1)
				if !tc.ambient {
					return nil, errors.New("ambient transport must not be called")
				}
				return reject(req)
			}}
			oldTransport, oldClient := http.DefaultTransport, http.DefaultClient
			ambientClient := &http.Client{Transport: ambient}
			http.DefaultTransport, http.DefaultClient = ambient, ambientClient
			t.Cleanup(func() { http.DefaultTransport, http.DefaultClient = oldTransport, oldClient })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for _, api := range tc.apis {
				beforeLocal, beforeAmbient := localCalls.Load(), ambientCalls.Load()
				provider := goai.GetApiProvider(api)
				if provider == nil || provider.StreamSimple == nil {
					t.Fatalf("missing simple stream provider %s", api)
				}
				model := &goai.Model{Api: api, ID: "test-model", Provider: "test-provider", BaseURL: base, ContextWindow: 8192, MaxTokens: 256}
				retry := goai.NoRetryConfig()
				opts := &goai.StreamOptions{APIKey: "test-key", RetryConfig: &retry, HTTPTransport: local}
				if tc.ambient {
					opts.HTTPTransport = nil
				}
				if api == goai.ApiAzureOpenAIResponses {
					opts.AzureBaseURL = base
				}
				if api == goai.ApiOpenAICodexResponses {
					opts.APIKey = "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"https://api.openai.com/auth":{"chatgpt_account_id":"account"}}`)) + ".signature"
					opts.Transport = goai.TransportSSE
				}
				var terminal *goai.ErrorEvent
				for event := range provider.StreamSimple(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, opts) {
					if failure, ok := event.(*goai.ErrorEvent); ok {
						terminal = failure
					}
				}
				wantLocal, wantAmbient := int32(1), int32(0)
				if tc.ambient {
					wantLocal, wantAmbient = 0, 1
				}
				if local, ambient := localCalls.Load()-beforeLocal, ambientCalls.Load()-beforeAmbient; local != wantLocal || ambient != wantAmbient {
					t.Errorf("%s transport calls = local:%d ambient:%d; want local:%d ambient:%d", api, local, ambient, wantLocal, wantAmbient)
				}
				if tc.name == "google-custom" && (terminal == nil || terminal.Err == nil || !strings.Contains(terminal.Err.Error(), rejection)) {
					t.Errorf("%s terminal = %+v; want provider rejection %q", api, terminal, rejection)
				}
			}
			wantLocal, wantAmbient := int32(len(tc.apis)), int32(0)
			if tc.ambient {
				wantLocal, wantAmbient = 0, wantLocal
			}
			if tc.image {
				model := &images.ImagesModel{ID: "test-model", Api: images.ImagesApiOpenRouter, Provider: images.ImagesProviderOpenRouter, BaseURL: base, Output: []string{"image"}}
				if _, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{APIKey: "test-key", Context: ctx, HTTPClient: &http.Client{Transport: local}}); err != nil {
					t.Fatal(err)
				}
				wantLocal = 1
			}
			if localCalls.Load() != wantLocal || ambientCalls.Load() != wantAmbient || bypasses.Load() != 0 {
				t.Fatalf("transport calls = local:%d ambient:%d loopback:%d; want local:%d ambient:%d loopback:0", localCalls.Load(), ambientCalls.Load(), bypasses.Load(), wantLocal, wantAmbient)
			}
			if http.DefaultTransport != ambient || http.DefaultClient != ambientClient || ambientClient.Transport != ambient {
				t.Fatal("request-local option replaced ambient transport")
			}
		})
	}
}
