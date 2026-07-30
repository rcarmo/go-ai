package openairesponses

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAIResponsesRawStatusAndMissingTerminalError(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	body := strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	ch := make(chan goai.Event, 10)
	processStream(body, model, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if done.Message.RawStopReason != "completed" || done.Message.StopReason != goai.StopReasonStop {
				t.Fatalf("message=%#v", done.Message)
			}
		}
	}
	missing := strings.NewReader("data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"id\":\"item_1\"}}\n\n")
	ch = make(chan goai.Event, 10)
	processStream(missing, model, ch)
	close(ch)
	for ev := range ch {
		if e, ok := ev.(*goai.ErrorEvent); ok && strings.Contains(e.Err.Error(), "before a terminal response event") {
			return
		}
	}
	t.Fatal("expected terminal response error")
}
