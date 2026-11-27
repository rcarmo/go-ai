package goai_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// The compatibility flags describe the wire contract; the model registry is
// verified separately against the published catalog.
func TestCloudflareGatewayWorkersCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prompt  string
		limit   *int
		session string
	}{
		{name: "conservative-fields", prompt: "You are helpful.", limit: new(1234)},
		{name: "provider-auth"},
		{name: "session-affinity", session: "session-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{
				ID: "workers-ai/@cf/example/chat", Provider: goai.ProviderCloudflareAIGateway, Api: goai.ApiOpenAICompletions,
				BaseURL:   "https://gateway.ai.cloudflare.com/v1/{CLOUDFLARE_ACCOUNT_ID}/{CLOUDFLARE_GATEWAY_ID}/compat",
				Reasoning: true, ContextWindow: 10240, MaxTokens: 4096,
				CompletionsCompat: &goai.OpenAICompletionsCompat{
					SupportsStore: new(false), SupportsReasoningEffort: new(false), MaxTokensField: "max_tokens",
					SendSessionAffinityHeaders: new(true), SupportsLongCacheRetention: new(false),
				},
			}
			var url string
			var header http.Header
			var wire map[string]json.RawMessage
			opts := &goai.StreamOptions{
				Env:       goai.ProviderEnv{"CLOUDFLARE_ACCOUNT_ID": "account-id", "CLOUDFLARE_GATEWAY_ID": "gateway-id", "CLOUDFLARE_API_KEY": "cf-token"},
				Reasoning: new(goai.ThinkingHigh), MaxTokens: tc.limit, SessionID: tc.session,
				HTTPTransport: providerRetryTransportMain(func(req *http.Request) (*http.Response, error) {
					defer req.Body.Close()
					url, header = req.URL.String(), req.Header.Clone()
					if err := json.NewDecoder(req.Body).Decode(&wire); err != nil {
						return nil, err
					}
					res := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n"))}
					res.Header.Set("Content-Type", "text/event-stream")
					return res, nil
				}),
			}
			done := false
			for evt := range goai.Stream(t.Context(), model, &goai.Context{SystemPrompt: tc.prompt, Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
				switch event := evt.(type) {
				case *goai.ErrorEvent:
					t.Errorf("stream: %v", event.Err)
				case *goai.DoneEvent:
					done = true
				}
			}
			if !done || url != "https://gateway.ai.cloudflare.com/v1/account-id/gateway-id/compat/chat/completions" {
				t.Fatalf("completed=%v, url=%q", done, url)
			}
			if got := header.Values("Authorization"); len(got) != 0 {
				t.Errorf("unexpected standard authorization: %v", got)
			}
			if got := header.Get("cf-aig-authorization"); got != "Bearer cf-token" {
				t.Errorf("gateway authorization: %q", got)
			}
			if tc.limit != nil {
				var messages []struct{ Role string }
				if err := json.Unmarshal(wire["messages"], &messages); err != nil || len(messages) == 0 || messages[0].Role != "system" {
					t.Errorf("system role: %s, %v", wire["messages"], err)
				}
				if got := string(wire["max_tokens"]); got != "1234" {
					t.Errorf("max_tokens: %q", got)
				}
				for _, key := range []string{"max_completion_tokens", "reasoning_effort", "store"} {
					if _, exists := wire[key]; exists {
						t.Errorf("unexpected %s: %s", key, wire[key])
					}
				}
			}
			if tc.session != "" {
				for _, key := range []string{"session_id", "x-client-request-id", "x-session-affinity"} {
					if got := header.Get(key); got != "session-1" {
						t.Errorf("%s = %q, want session-1", key, got)
					}
				}
			}
		})
	}
}
