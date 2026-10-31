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
		if failure, ok := ev.(*goai.ErrorEvent); ok {
			if failure.Error.RawStopReason != "unmapped_error" || failure.Error.StopReason != goai.StopReasonError || failure.Error.ErrorMessage != "Provider stopped with: unmapped_error" {
				t.Fatalf("message=%#v", failure.Error)
			}
			return
		}
	}
	t.Fatal("missing error")
}

func TestMistral110ServerErrorIsRetryable(t *testing.T) {
	body := strings.NewReader("data: {\"choices\":[{\"finish_reason\":\"error\",\"delta\":{}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":0,\"total_tokens\":1}}\n\n")
	ch := make(chan goai.Event, 8)
	processSSEStream(body, &goai.Model{ID: "mistral-test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, goai.NewAssistantEventSender(ch))
	close(ch)
	for event := range ch {
		if failure, ok := event.(*goai.ErrorEvent); ok {
			if failure.Error.RawStopReason != "error" || failure.Error.ErrorMessage != "Provider stopped with: error (server error)" || !goai.IsRetryableAssistantError(failure.Error) {
				t.Fatal("1.1.0 server stop must retry", failure.Error)
			}
			return
		}
	}
	t.Fatal("missing error")
}
