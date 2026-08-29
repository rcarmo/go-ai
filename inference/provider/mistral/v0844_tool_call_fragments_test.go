package mistral

import (
	"io"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0844MistralMergesIndexedToolCallFragmentsWithoutRepeatedID(t *testing.T) {
	body := io.NopCloser(io.MultiReader(
		strings.NewReader("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"abc123456\",\"function\":{\"name\":\"lookup\",\"arguments\":\"{\\\"query\\\":\"}}]}}]}\n\n"),
		strings.NewReader("data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"\",\"arguments\":\"\\\"pi\\\"}\"}}]}}]}\n\n"),
		strings.NewReader("data: {\"choices\":[{\"finish_reason\":\"tool_calls\",\"delta\":{}}]}\n\n"),
		strings.NewReader("data: [DONE]\n\n"),
	))
	ch := make(chan goai.Event, 16)
	processSSEStream(body, &goai.Model{ID: "mistral-test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, ch)
	close(ch)

	var done *goai.Message
	for ev := range ch {
		if d, ok := ev.(*goai.DoneEvent); ok {
			done = d.Message
		}
	}
	if done == nil {
		t.Fatal("missing done")
	}
	if len(done.Content) != 1 || done.Content[0].Type != "toolCall" {
		t.Fatalf("content=%#v", done.Content)
	}
	call := done.Content[0]
	if call.ID != "abc123456" || call.Name != "lookup" || call.Arguments["query"] != "pi" {
		t.Fatalf("tool call=%#v", call)
	}
	if done.StopReason != goai.StopReasonToolUse {
		t.Fatalf("stopReason=%q", done.StopReason)
	}
}
