package anthropic

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0843AnthropicFallbacksAndServerSideBeta(t *testing.T) {
	model := &goai.Model{ID: "claude-fable-5", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, MaxTokens: 4096, AnthropicCompat: &goai.AnthropicMessagesCompat{AllowedFallbackModels: []goai.AnthropicAllowedFallbackModel{{Provider: goai.ProviderAnthropic, Model: "claude-opus-5", Cost: goai.ModelCost{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}}}}}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	req := buildRequest(model, ctx, &goai.StreamOptions{})
	if len(req.Fallbacks) != 1 || req.Fallbacks[0].Model != "claude-opus-5" {
		t.Fatalf("fallbacks=%#v", req.Fallbacks)
	}
	data, _ := json.Marshal(req)
	if string(data) == "" || !json.Valid(data) {
		t.Fatalf("invalid request json: %s", data)
	}

	// Header beta is assembled in stream path; exercise the same compat predicate.
	if len(getAnthropicCompat(model).allowedFallbackModels) != 1 {
		t.Fatal("expected fallback compat to be present")
	}
}

func TestV0843AnthropicFallbackPricingUsesReturnedModel(t *testing.T) {
	fallbackCost := goai.ModelCost{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}
	model := &goai.Model{ID: "claude-fable-5", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, Cost: goai.ModelCost{Input: 1, Output: 1}, AnthropicCompat: &goai.AnthropicMessagesCompat{AllowedFallbackModels: []goai.AnthropicAllowedFallbackModel{{Provider: goai.ProviderAnthropic, Model: "claude-opus-5", Cost: fallbackCost}}}}
	body := `event: message_start
data: {"message":{"id":"msg_1","model":"claude-opus-5","usage":{"input_tokens":10}}}

event: content_block_start
data: {"index":0,"content_block":{"type":"text","text":"ok"}}

event: content_block_stop
data: {"index":0}

event: message_delta
data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}

event: message_stop
data: {}

`
	ch := make(chan goai.Event, 16)
	processAnthropicStream(strings.NewReader(body), model, nil, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if done.Message.Model != "claude-opus-5" {
				t.Fatalf("model=%q", done.Message.Model)
			}
			if done.Message.Usage.Cost.Input != 0.00005 || done.Message.Usage.Cost.Output != 0.00005 {
				t.Fatalf("fallback cost not applied: %#v", done.Message.Usage.Cost)
			}
			return
		}
	}
	t.Fatal("missing done")
}

func TestV0843PiUserAgentDefaultAndOverrideHeader(t *testing.T) {
	h := make(http.Header)
	h.Set("Accept", "text/event-stream")
	goai.ApplyDefaultHeaders(h, map[string]string{"User-Agent": "model-agent"})
	goai.ApplyDefaultHeaders(h, goai.PiUserAgentHeader())
	if h.Get("User-Agent") != "model-agent" {
		t.Fatalf("model header should override default pi user-agent, got %q", h.Get("User-Agent"))
	}
	goai.ApplyHeaders(h, map[string]string{"User-Agent": "caller-agent"})
	if h.Get("User-Agent") != "caller-agent" {
		t.Fatalf("caller header should override user-agent, got %q", h.Get("User-Agent"))
	}
}
