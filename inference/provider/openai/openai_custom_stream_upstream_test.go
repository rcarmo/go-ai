package openai

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsCustomGrammarStreamReconstructsInput(t *testing.T) {
	body := strings.NewReader("data: {\"id\":\"chatcmpl_1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"custom\",\"custom\":{\"name\":\"grammar\",\"input\":\"ab\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"type\":\"custom\",\"custom\":{\"input\":\"c\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"finish_reason\":\"tool_calls\",\"delta\":{}}]}\n\n")
	ch := make(chan goai.Event, 20)
	processSSEStream(body, &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}, ch)
	close(ch)
	var deltas []string
	var done *goai.DoneEvent
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.ToolCallDeltaEvent:
			deltas = append(deltas, e.Delta)
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if got := strings.Join(deltas, ""); got != `{"input":"abc"}` {
		t.Fatalf("custom deltas=%q", got)
	}
	if done == nil || done.Message.StopReason != goai.StopReasonToolUse || len(done.Message.Content) != 1 {
		t.Fatalf("done=%#v", done)
	}
	args := done.Message.Content[0].Arguments
	if args["input"] != "abc" {
		t.Fatalf("args=%#v", args)
	}
}
