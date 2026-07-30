package mistral

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMistralRawStopReason(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"finish_reason\":\"unmapped_error\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\n")
	ch := make(chan goai.Event, 8)
	processSSEStream(body, &goai.Model{ID: "mistral-test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if done.Message.RawStopReason != "unmapped_error" || done.Message.StopReason != goai.StopReasonError || done.Message.ErrorMessage != "Provider stopped with: unmapped_error" {
				t.Fatalf("message=%#v", done.Message)
			}
			return
		}
	}
	t.Fatal("missing done")
}
