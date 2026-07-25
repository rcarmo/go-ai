package anthropic

import (
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicAuthTokenEnvUsesBearerAuthorization(t *testing.T) {
	var gotAuth, gotAPIKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()
	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: server.URL, Input: []string{"text"}, ContextWindow: 1000, MaxTokens: 100}
	opts := &goai.StreamOptions{Env: goai.ProviderEnv{"ANTHROPIC_AUTH_TOKEN": "gateway-token"}}
	for ev := range streamAnthropic(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, opts) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if gotAuth != "Bearer gateway-token" || gotAPIKey != "" {
		t.Fatalf("Authorization=%q X-Api-Key=%q", gotAuth, gotAPIKey)
	}
}
