package openai

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsRawStopReason(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"index\":0,\"finish_reason\":\"content_filter\",\"delta\":{}}]}\n\n")
	ch := make(chan goai.Event, 8)
	processSSEStream(body, &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if done.Message.RawStopReason != "content_filter" || done.Message.StopReason != goai.StopReasonError || done.Message.ErrorMessage != "Provider finish_reason: content_filter" {
				t.Fatalf("message=%#v", done.Message)
			}
			return
		}
	}
	t.Fatal("missing done")
}
