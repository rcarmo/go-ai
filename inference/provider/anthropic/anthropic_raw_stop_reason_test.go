package anthropic

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicRawStopReasonAndPendingTerminalError(t *testing.T) {
	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages}
	body := strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"refusal\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	ch := make(chan goai.Event, 10)
	processAnthropicStream(body, model, nil, "", ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if done.Message.RawStopReason != "refusal" || done.Message.StopReason != goai.StopReasonError {
				t.Fatalf("message=%#v", done.Message)
			}
			break
		}
	}

	pending := strings.NewReader("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_2\",\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	ch = make(chan goai.Event, 10)
	processAnthropicStream(pending, model, nil, "", ch)
	close(ch)
	for ev := range ch {
		if e, ok := ev.(*goai.ErrorEvent); ok && strings.Contains(e.Err.Error(), "without a stop reason") {
			return
		}
	}
	t.Fatal("expected pending terminal error")
}
