package goai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

func TestResolveCloudflareBaseURLPreservesUnresolvedPlaceholders(t *testing.T) {
	model := &goai.Model{ID: "model", Provider: goai.ProviderCloudflareAIGateway, BaseURL: "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/openai"}
	got := goai.ResolveCloudflareBaseURL(model, goai.ProviderEnv{"CLOUDFLARE_ACCOUNT_ID": "account"})
	want := "https://gateway.ai.cloudflare.com/v1/account/{CLOUDFLARE_GATEWAY_ID}/openai"
	if got != want {
		t.Fatalf("resolved Cloudflare URL=%q, want %q", got, want)
	}
}

func TestCloudflareBaseURLResolvedAndUnresolvedThroughDispatch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		api        goai.Api
		invoke     func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event
		terminal   string
		wantPath   string
		wantHeader string
	}{
		{
			name: "openai completions stream resolved",
			api:  goai.ApiOpenAICompletions,
			invoke: func(ctx context.Context, model *goai.Model, conv *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
				return goai.Stream(ctx, model, conv, opts)
			},
			terminal:   "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n",
			wantPath:   "/account/gateway/chat/completions",
			wantHeader: "Bearer cf-key",
		},
		{
			name: "openai responses streamSimple resolved",
			api:  goai.ApiOpenAIResponses,
			invoke: func(ctx context.Context, model *goai.Model, conv *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
				reasoning := goai.ThinkingLevel(goai.ThinkingOff)
				copyOpts := *opts
				copyOpts.Reasoning = &reasoning
				return goai.Stream(ctx, model, conv, &copyOpts)
			},
			terminal:   "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n",
			wantPath:   "/account/gateway/responses",
			wantHeader: "Bearer cf-key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotCfAIG string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotCfAIG = r.Header.Get("cf-aig-authorization")
				w.Header().Set("content-type", "text/event-stream")
				_, _ = w.Write([]byte(tc.terminal))
			}))
			defer server.Close()

			model := &goai.Model{ID: "model", Provider: goai.ProviderCloudflareAIGateway, Api: tc.api, BaseURL: server.URL + "/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}"}
			opts := &goai.StreamOptions{APIKey: "cf-key", Env: goai.ProviderEnv{"CLOUDFLARE_ACCOUNT_ID": "account", "CLOUDFLARE_GATEWAY_ID": "gateway"}}
			for range tc.invoke(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path=%q, want %q", gotPath, tc.wantPath)
			}
			if gotCfAIG != tc.wantHeader {
				t.Fatalf("cf-aig-authorization=%q, want %q", gotCfAIG, tc.wantHeader)
			}
		})
	}
}

func TestCloudflareBaseURLUnresolvedPlaceholdersReachDispatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		api      goai.Api
		invoke   func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event
		terminal string
		wantPath string
	}{
		{
			name: "openai completions stream unresolved",
			api:  goai.ApiOpenAICompletions,
			invoke: func(ctx context.Context, model *goai.Model, conv *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
				return goai.Stream(ctx, model, conv, opts)
			},
			terminal: "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n",
			wantPath: "/{CLOUDFLARE_GATEWAY_ID}/chat/completions",
		},
		{
			name: "openai responses streamSimple unresolved",
			api:  goai.ApiOpenAIResponses,
			invoke: func(ctx context.Context, model *goai.Model, conv *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
				reasoning := goai.ThinkingLevel(goai.ThinkingOff)
				copyOpts := *opts
				copyOpts.Reasoning = &reasoning
				return goai.Stream(ctx, model, conv, &copyOpts)
			},
			terminal: "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":0,\"output_tokens\":0,\"total_tokens\":0}}}\n\n",
			wantPath: "/{CLOUDFLARE_GATEWAY_ID}/responses",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				w.Header().Set("content-type", "text/event-stream")
				_, _ = w.Write([]byte(tc.terminal))
			}))
			defer server.Close()

			model := &goai.Model{ID: "model", Provider: goai.ProviderCloudflareAIGateway, Api: tc.api, BaseURL: server.URL + "/{CLOUDFLARE_GATEWAY_ID}"}
			for range tc.invoke(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "cf-key"}) {
			}
			if gotPath != tc.wantPath {
				t.Fatalf("path=%q, want %q", gotPath, tc.wantPath)
			}
		})
	}
}
