package goai_test

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/anthropic"
	_ "github.com/rcarmo/go-ai/inference/provider/google"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

// Source: pi v1.6.1 provider-error-response.test.ts:83–104.
func TestSourceV161ProviderErrorResponse(t *testing.T) {
	for _, adapter := range []struct {
		api         goai.Api
		path        string
		wantHeaders bool
	}{
		{goai.ApiOpenAICompletions, "/chat/completions", true},
		{goai.ApiOpenAIResponses, "/responses", true},
		{goai.ApiAzureOpenAIResponses, "/deployments/test-model/responses?api-version=v1", true},
		{goai.ApiAnthropicMessages, "/v1/messages", true},
		{goai.ApiGoogleGenerativeAI, "/models/test-model:streamGenerateContent?alt=sse", false},
	} {
		for _, row := range []struct {
			name         string
			maxRetries   int
			nativeConfig bool
			want         []int
		}{
			{"failed-retry", 1, false, []int{429, 429}},
			{"no-retry", 0, false, []int{429}},
			{"native-config-precedence", 1, true, []int{429}},
		} {
			t.Run(string(adapter.api)+"/"+row.name, func(t *testing.T) {
				const base = "https://provider.invalid"
				model := &goai.Model{ID: "test-model", Name: "Test Model", Provider: "test", Api: adapter.api,
					BaseURL: base, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100}
				var requests, terminal int
				var initial []byte
				var statuses, callbacksAt []int
				var headers []map[string]string
				var reason goai.StopReason
				transport := &fetchOptionWire{roundTrip: func(r *http.Request) (*http.Response, error) {
					defer r.Body.Close()
					requests++
					if r.Method != http.MethodPost || r.URL.String() != base+adapter.path {
						return nil, fmt.Errorf("unexpected scripted request %s %s", r.Method, r.URL)
					}
					body, err := io.ReadAll(r.Body)
					if err != nil {
						return nil, err
					}
					if requests == 1 {
						initial = body
					} else if !slices.Equal(initial, body) {
						return nil, fmt.Errorf("retry did not replay the request body")
					}
					return &http.Response{StatusCode: http.StatusTooManyRequests,
						Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"0"}, "X-Request-Id": {"req-1"}},
						Body:   io.NopCloser(strings.NewReader(`{"error":{"message":"rate limited","type":"rate_limit_error","code":429}}`))}, nil
				}}
				opts := &goai.StreamOptions{APIKey: "test-key", AzureBaseURL: base, AzureAPIVersion: "v1", AzureDeploymentName: model.ID,
					MaxRetries: &row.maxRetries, HTTPTransport: transport,
					OnResponse: func(status int, fields map[string]string, source *goai.Model) {
						if source != model {
							t.Error("response hook received the wrong model")
						}
						statuses = append(statuses, status)
						callbacksAt = append(callbacksAt, requests)
						headers = append(headers, fields)
					},
				}
				if row.nativeConfig {
					cfg := goai.NoRetryConfig()
					opts.RetryConfig = &cfg
				}
				for event := range goai.Stream(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						terminal++
						reason = e.Reason
					case *goai.DoneEvent:
						terminal++
						reason = e.Reason
					}
				}
				if reason != goai.StopReasonError || terminal != 1 {
					t.Errorf("terminal events=%d stopReason=%q, want one error", terminal, reason)
				}
				if requests != len(row.want) {
					t.Errorf("requests=%d, want %d", requests, len(row.want))
				}
				if !slices.Equal(statuses, row.want) {
					t.Errorf("callback statuses=%v, want %v", statuses, row.want)
				}
				for i, request := range callbacksAt {
					if request != i+1 {
						t.Errorf("callback %d occurred at request %d, want %d", i, request, i+1)
					}
				}
				for i, fields := range headers {
					if !adapter.wantHeaders && !row.nativeConfig {
						if fields == nil || len(fields) != 0 {
							t.Errorf("callback %d headers=%#v, want an empty map", i, fields)
						}
						continue
					}
					// The native hook uses canonical HTTP header names.
					if fields["Retry-After"] != "0" {
						t.Errorf("callback %d retry-after=%q, want 0", i, fields["Retry-After"])
					}
					if fields["X-Request-Id"] != "req-1" {
						t.Errorf("callback %d x-request-id=%q, want req-1", i, fields["X-Request-Id"])
					}
				}
			})
		}
	}
}
