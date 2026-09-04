package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0850AnthropicModelBetaHeaderOverridesGeneratedBetas(t *testing.T) {
	var beta string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beta = r.Header.Get("Anthropic-Beta")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte("data: {\"message\":{\"id\":\"msg_1\",\"model\":\"claude-fable-5-1\",\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer server.Close()

	model := v0850ManagedEffortModel()
	model.BaseURL = server.URL
	model.Headers = map[string]string{"Anthropic-Beta": "custom-beta"}
	for range streamAnthropic(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("one")}}, &goai.StreamOptions{APIKey: "key"}) {
	}
	if beta != "custom-beta" {
		t.Fatalf("Anthropic-Beta=%q, want model override", beta)
	}
}
