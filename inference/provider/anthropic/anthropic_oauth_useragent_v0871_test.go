package anthropic

import (
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type anthropicAuthHeaders struct {
	Authorization string
	XAPIKey       string
	UserAgent     string
	XApp          string
}

func captureAnthropicAuthHeaders(t *testing.T, opts *goai.StreamOptions) anthropicAuthHeaders {
	t.Helper()
	var got anthropicAuthHeaders
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = anthropicAuthHeaders{
			Authorization: r.Header.Get("Authorization"),
			XAPIKey:       r.Header.Get("X-Api-Key"),
			UserAgent:     r.Header.Get("User-Agent"),
			XApp:          r.Header.Get("x-app"),
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: server.URL, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100}
	for ev := range streamAnthropic(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	return got
}

func TestV0871AnthropicOrdinaryAPIKeyUsesXAPIKeyWithoutClaudeCodeHeaders(t *testing.T) {
	got := captureAnthropicAuthHeaders(t, &goai.StreamOptions{APIKey: "sk-ant-api03-test"})
	if got.Authorization != "" || got.XAPIKey != "sk-ant-api03-test" || got.UserAgent == "claude-cli/2.1.280" || got.XApp != "" {
		t.Fatalf("ordinary API key headers=%#v", got)
	}
}

func TestV0871AnthropicExplicitAuthOverrideRemainsAuthoritative(t *testing.T) {
	got := captureAnthropicAuthHeaders(t, &goai.StreamOptions{APIKey: "sk-ant-oat-test", Headers: map[string]string{"Authorization": "Bearer explicit"}})
	if got.Authorization != "Bearer explicit" || got.XAPIKey != "" || got.UserAgent == "claude-cli/2.1.280" || got.XApp != "" {
		t.Fatalf("explicit auth override headers=%#v", got)
	}
}

func TestV0871AnthropicOAuthTokenUsesBearerAndClaudeCodeUserAgent(t *testing.T) {
	got := captureAnthropicAuthHeaders(t, &goai.StreamOptions{APIKey: "sk-ant-oat-test"})
	if got.Authorization != "Bearer sk-ant-oat-test" || got.XAPIKey != "" || got.UserAgent != "claude-cli/2.1.280" || got.XApp != "cli" {
		t.Fatalf("oauth headers=%#v", got)
	}
}
