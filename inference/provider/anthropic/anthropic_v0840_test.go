package anthropic

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicContentBlockStartInitialContentAndSignature(t *testing.T) {
	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages}
	body := strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg\",\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"Initial text\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" plus delta\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"Initial thinking\",\"signature\":\"sig\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\" plus delta\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"-more\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	ch := make(chan goai.Event, 16)
	processAnthropicStream(body, model, nil, ch)
	close(ch)
	var done *goai.DoneEvent
	for ev := range ch {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
		if d, ok := ev.(*goai.DoneEvent); ok {
			done = d
		}
	}
	if done == nil || len(done.Message.Content) != 2 {
		t.Fatalf("missing done/content: %#v", done)
	}
	if got := done.Message.Content[0].Text; got != "Initial text plus delta" {
		t.Fatalf("text=%q", got)
	}
	if got := done.Message.Content[1].Thinking; got != "Initial thinking plus delta" {
		t.Fatalf("thinking=%q", got)
	}
	if got := done.Message.Content[1].ThinkingSignature; got != "sig-more" {
		t.Fatalf("signature=%q", got)
	}
}
