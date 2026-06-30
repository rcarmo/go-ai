package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestStreamAnthropicCapturesThinkingTokensAsUsageReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":6}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"thinking\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"hmm\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":5,\"output_tokens_details\":{\"thinking_tokens\":3}}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: server.URL + "/v1", MaxTokens: 1024, Cost: goai.ModelCost{}}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}
	var done *goai.Message
	for ev := range streamAnthropic(context.Background(), model, ctx, &goai.StreamOptions{APIKey: "test-key"}) {
		if e, ok := ev.(*goai.DoneEvent); ok {
			done = e.Message
		}
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if done == nil || done.Usage == nil {
		t.Fatalf("missing done/usage: %#v", done)
	}
	if done.Usage.Reasoning != 3 {
		t.Fatalf("reasoning=%d, want 3", done.Usage.Reasoning)
	}
	if done.Usage.TotalTokens != 23 {
		t.Fatalf("totalTokens=%d, want input+output+cacheRead+cacheWrite=23", done.Usage.TotalTokens)
	}
}
