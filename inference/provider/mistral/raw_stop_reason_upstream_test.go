package mistral

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMistralRawStopReason(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"finish_reason\":\"unmapped_error\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\n")
	ch := make(chan goai.Event, 8)
	processSSEStream(body, &goai.Model{ID: "mistral-test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, goai.NewAssistantEventSender(ch))
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

func TestMistral110ServerErrorIsRetryable(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"finish_reason\":\"error\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\n")
	ch := make(chan goai.Event, 8)
	processSSEStream(body, &goai.Model{ID: "mistral-test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, goai.NewAssistantEventSender(ch))
	close(ch)
	for event := range ch {
		if done, ok := event.(*goai.DoneEvent); ok {
			if done.Message.RawStopReason != "error" || done.Message.ErrorMessage != "Provider stopped with: error (server error)" || !goai.IsRetryableAssistantError(done.Message) {
				t.Fatal("1.1.0 server stop must retry", done.Message)
			}
			return
		}
	}
	t.Fatal("missing done")
}
