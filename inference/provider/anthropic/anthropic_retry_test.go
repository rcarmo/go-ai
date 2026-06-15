package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestStreamAnthropicParsesOneHourCacheWriteUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":6,\"cache_creation\":{\"ephemeral_1h_input_tokens\":4}}}}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "claude-sonnet-4-20250514", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: server.URL, Cost: goai.ModelCost{Input: 3, Output: 15, CacheRead: 0.3, CacheWrite: 3.75}}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}

	var done *goai.DoneEvent
	for ev := range streamAnthropic(context.Background(), model, convCtx, &goai.StreamOptions{APIKey: "test-key"}) {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if done == nil || done.Message == nil {
		t.Fatalf("expected done event, got %#v", done)
	}
	usage := done.Message.Usage
	if usage.CacheWrite != 6 || usage.CacheWrite1h != 4 {
		t.Fatalf("unexpected cache usage: %#v", usage)
	}
	wantCacheWriteCost := (2*3.75 + 4*6.0) / 1_000_000
	if usage.Cost.CacheWrite < wantCacheWriteCost-0.0000001 || usage.Cost.CacheWrite > wantCacheWriteCost+0.0000001 {
		t.Fatalf("unexpected cache write cost: got %f want %f", usage.Cost.CacheWrite, wantCacheWriteCost)
	}
}

func TestStreamAnthropicRetries429AndSucceeds(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":1}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {}\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "claude-sonnet-4-20250514", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: server.URL}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	opts := &goai.StreamOptions{APIKey: "test-key", RetryConfig: &goai.RetryConfig{MaxRetries: 1, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, BackoffMultiplier: 1, ConnectTimeout: time.Second, RequestTimeout: time.Second}}

	var done *goai.DoneEvent
	for ev := range streamAnthropic(context.Background(), model, convCtx, opts) {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if done == nil || done.Message == nil || done.Message.StopReason != goai.StopReasonStop {
		t.Fatalf("expected successful completion, got %#v", done)
	}
}
