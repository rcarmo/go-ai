package openaicodex

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestCodexResponsesCustomToolStreamReconstructsInput(t *testing.T) {
	body := strings.NewReader("data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"custom_tool_call\",\"call_id\":\"call_1\",\"id\":\"ctc_1\",\"name\":\"grammar\"}}\n\n" +
		"data: {\"type\":\"response.custom_tool_call_input.delta\",\"delta\":\"ab\"}\n\n" +
		"data: {\"type\":\"response.custom_tool_call_input.done\",\"input\":\"abc\"}\n\n" +
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"custom_tool_call\",\"call_id\":\"call_1\",\"id\":\"ctc_1\",\"name\":\"grammar\",\"input\":\"abc\"}}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	ch := make(chan goai.Event, 20)
	processCodexSSE(body, &goai.Model{ID: "gpt-5.4-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses}, ch, nil)
	close(ch)
	var done *goai.DoneEvent
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if done == nil || done.Message.StopReason != goai.StopReasonToolUse || len(done.Message.Content) != 1 || done.Message.Content[0].Arguments["input"] != "abc" {
		t.Fatalf("done=%#v", done)
	}
}
