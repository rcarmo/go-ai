package google

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGoogleRawStopReason(t *testing.T) {
	body := strings.NewReader("data: {\"candidates\":[{\"finishReason\":\"MALFORMED_FUNCTION_CALL\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":0,\"totalTokenCount\":1}}\n\n")
	ch := make(chan goai.Event, 8)
	processStream(body, &goai.Model{ID: "gemini-test", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI}, goai.NewAssistantEventSender(ch))
	close(ch)
	for ev := range ch {
		if failure, ok := ev.(*goai.ErrorEvent); ok {
			if failure.Error == nil || failure.Err == nil {
				t.Fatal("missing error message or diagnostic")
			}
			if failure.Error.RawStopReason != "MALFORMED_FUNCTION_CALL" || failure.Error.StopReason != goai.StopReasonError || failure.Error.ErrorMessage != "Provider stopped with: MALFORMED_FUNCTION_CALL" {
				t.Fatalf("message=%#v", failure.Error)
			}
			return
		}
	}
	t.Fatal("missing error event")
}

func TestGoogleRawStopReasonWithToolCallDoesNotMaskErrorFinishReason(t *testing.T) {
	body := strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"lookup\",\"args\":{}}}]},\"finishReason\":\"MALFORMED_FUNCTION_CALL\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":0,\"totalTokenCount\":1}}\n\n")
	ch := make(chan goai.Event, 8)
	processStream(body, &goai.Model{ID: "gemini-test", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI}, goai.NewAssistantEventSender(ch))
	close(ch)
	for ev := range ch {
		if failure, ok := ev.(*goai.ErrorEvent); ok {
			if failure.Error == nil || failure.Err == nil {
				t.Fatal("missing error message or diagnostic")
			}
			if failure.Error.RawStopReason != "MALFORMED_FUNCTION_CALL" || failure.Error.StopReason != goai.StopReasonError {
				t.Fatalf("message=%#v", failure.Error)
			}
			return
		}
	}
	t.Fatal("missing error event")
}
