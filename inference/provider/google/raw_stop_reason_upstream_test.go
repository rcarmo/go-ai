package google

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestGoogleRawStopReason(t *testing.T) {
	body := strings.NewReader("data: {\"candidates\":[{\"finishReason\":\"MALFORMED_FUNCTION_CALL\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":0,\"totalTokenCount\":1}}\n\n")
	ch := make(chan goai.Event, 8)
	processStream(body, &goai.Model{ID: "gemini-test", Provider: goai.ProviderGoogle, Api: goai.ApiGoogleGenerativeAI}, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if done.Message.RawStopReason != "MALFORMED_FUNCTION_CALL" || done.Message.StopReason != goai.StopReasonError || done.Message.ErrorMessage != "Provider stopped with: MALFORMED_FUNCTION_CALL" {
				t.Fatalf("message=%#v", done.Message)
			}
			return
		}
	}
	t.Fatal("missing done")
}
