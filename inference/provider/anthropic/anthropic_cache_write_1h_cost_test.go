package anthropic

import (
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicCacheWrite1hCostPrices1hPortionAt2xInputAndRestAt5mRate(t *testing.T) {
	message := runAnthropicCacheWrite1hCostEvents(t, map[string]int{"ephemeral_5m_input_tokens": 600_000, "ephemeral_1h_input_tokens": 400_000})
	if message.Usage.CacheWrite != 1_000_000 {
		t.Fatalf("cacheWrite=%d, want 1000000", message.Usage.CacheWrite)
	}
	if message.Usage.CacheWrite1h != 400_000 {
		t.Fatalf("cacheWrite1h=%d, want 400000", message.Usage.CacheWrite1h)
	}
	if math.Abs(message.Usage.Cost.CacheWrite-7.75) > 1e-10 {
		t.Fatalf("cacheWrite cost=%f, want 7.75", message.Usage.Cost.CacheWrite)
	}
}

func TestAnthropicCacheWrite1hCostFallsBackTo5mRateWhenNoBreakdownReported(t *testing.T) {
	message := runAnthropicCacheWrite1hCostEvents(t, nil)
	if message.Usage.CacheWrite != 1_000_000 {
		t.Fatalf("cacheWrite=%d, want 1000000", message.Usage.CacheWrite)
	}
	if message.Usage.CacheWrite1h != 0 {
		t.Fatalf("cacheWrite1h=%d, want 0", message.Usage.CacheWrite1h)
	}
	if math.Abs(message.Usage.Cost.CacheWrite-6.25) > 1e-10 {
		t.Fatalf("cacheWrite cost=%f, want 6.25", message.Usage.Cost.CacheWrite)
	}
}

func runAnthropicCacheWrite1hCostEvents(t *testing.T, cacheCreation map[string]int) *goai.Message {
	t.Helper()
	body := io.NopCloser(strings.NewReader(anthropicCacheWrite1hSSE(cacheCreation)))
	ch := make(chan goai.Event, 16)
	model := &goai.Model{ID: "claude-opus-4-8", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, Cost: goai.ModelCost{Input: 5, Output: 15, CacheRead: 0.3, CacheWrite: 6.25}}
	processAnthropicStream(body, model, nil, "", ch)
	close(ch)
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			return e.Message
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	t.Fatal("expected DoneEvent")
	return nil
}

func anthropicCacheWrite1hSSE(cacheCreation map[string]int) string {
	cacheCreationJSON := ""
	if cacheCreation != nil {
		cacheCreationJSON = fmt.Sprintf(`,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d}`, cacheCreation["ephemeral_5m_input_tokens"], cacheCreation["ephemeral_1h_input_tokens"])
	}
	return fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"usage\":{\"input_tokens\":100,\"output_tokens\":0,\"cache_read_input_tokens\":0,\"cache_creation_input_tokens\":1000000%s}}}\n\n"+
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"+
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hi\"}}\n\n"+
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"+
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":100,\"output_tokens\":5,\"cache_read_input_tokens\":0,\"cache_creation_input_tokens\":1000000}}\n\n"+
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n", cacheCreationJSON)
}
