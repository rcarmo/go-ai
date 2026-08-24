package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0843StreamAnthropicFallbacksHeadersAndPricingEndToEnd(t *testing.T) {
	fallbackCost := goai.ModelCost{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}
	var captured map[string]any
	var beta, ua string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		beta = r.Header.Get("Anthropic-Beta")
		ua = r.Header.Get("User-Agent")
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\n"))
		_, _ = w.Write([]byte("data: {\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-5\",\"usage\":{\"input_tokens\":10}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\n"))
		_, _ = w.Write([]byte("data: {\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"ok\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\n"))
		_, _ = w.Write([]byte("data: {\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\n"))
		_, _ = w.Write([]byte("data: {\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\n"))
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "claude-fable-5", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: server.URL, MaxTokens: 4096, Cost: goai.ModelCost{Input: 1, Output: 1}, AnthropicCompat: &goai.AnthropicMessagesCompat{AllowedFallbackModels: []goai.AnthropicAllowedFallbackModel{{Provider: goai.ProviderAnthropic, Model: "claude-opus-5", Cost: fallbackCost}}}}
	var done *goai.Message
	for ev := range streamAnthropic(nilContextForAnthropicTest(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "key"}) {
		if e, ok := ev.(*goai.DoneEvent); ok {
			done = e.Message
		}
	}
	if got := captured["fallbacks"].([]any)[0].(map[string]any)["model"]; got != "claude-opus-5" {
		t.Fatalf("fallback model=%#v payload=%#v", got, captured)
	}
	if beta == "" || !containsForAnthropicTest(beta, serverSideFallbackBeta) {
		t.Fatalf("Anthropic-Beta=%q missing fallback beta", beta)
	}
	if ua != goai.PiUserAgent() {
		t.Fatalf("User-Agent=%q want %q", ua, goai.PiUserAgent())
	}
	if done == nil || done.Model != "claude-opus-5" || done.Usage.Cost.Input != 0.00005 || done.Usage.Cost.Output != 0.00005 {
		t.Fatalf("fallback pricing/model not applied: %#v", done)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ua = r.Header.Get("User-Agent"); w.WriteHeader(401) })
	for range streamAnthropic(nilContextForAnthropicTest(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: "key", Headers: map[string]string{"User-Agent": "custom-agent"}}) {
	}
	if ua != "custom-agent" {
		t.Fatalf("explicit User-Agent override=%q", ua)
	}
}

func containsForAnthropicTest(s, sub string) bool { return strings.Contains(s, sub) }
func nilContextForAnthropicTest() context.Context { return context.Background() }
