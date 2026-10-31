package openairesponses

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func responsesGrammarFixtureMain(size int, initial string) (string, string) {
	chunk := `chunk "quoted" \ ` + "\n" + strings.Repeat("x", 14)
	input := initial + strings.Repeat(chunk, size/len(chunk))
	var body strings.Builder
	emit := func(value any) {
		encoded, err := json.Marshal(value)
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(&body, "data: %s\n\n", encoded)
	}
	item := func(text string) map[string]any {
		return map[string]any{"type": "custom_tool_call", "call_id": "call_1", "id": "ctc_1", "name": "sample_tool", "input": text}
	}
	emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item(initial)})
	for offset := len(initial); offset < len(input); offset += len(chunk) {
		emit(map[string]any{"type": "response.custom_tool_call_input.delta", "output_index": 0, "item_id": "ctc_1", "delta": input[offset : offset+len(chunk)]})
	}
	emit(map[string]any{"type": "response.custom_tool_call_input.done", "output_index": 0, "item_id": "ctc_1", "input": input})
	emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item(input)})
	emit(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	return body.String(), input
}

func runResponsesGrammarMain(tb testing.TB, body, expected, initial string) {
	tb.Helper()
	events := make(chan goai.Event, 32)
	type result struct {
		json  string
		start any
		done  *goai.Message
		err   error
	}
	completed := make(chan result, 1)
	go func() {
		var delta strings.Builder
		var r result
		for event := range events {
			switch e := event.(type) {
			case *goai.ToolCallStartEvent:
				r.start = e.Partial.Content[e.ContentIndex].Arguments["payload"]
			case *goai.ToolCallDeltaEvent:
				delta.WriteString(e.Delta)
			case *goai.DoneEvent:
				r.done = e.Message
			case *goai.ErrorEvent:
				r.err = e.Err
			}
		}
		r.json = delta.String()
		completed <- r
	}()
	processStreamWithOptions(strings.NewReader(body), &goai.Model{ID: "gpt-test", Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI}, nil, goai.NewAssistantEventSender(events), responsesToolView{grammarToolInputProperties: map[string]string{"sample_tool": "payload"}})
	close(events)
	r := <-completed
	if r.err != nil || r.done == nil {
		tb.Fatal("grammar stream failed", r.err)
	}
	if r.start != initial {
		tb.Fatal("initial input lost", r.start, initial)
	}
	if len(r.done.Content) != 1 || r.done.Content[0].Arguments["payload"] != expected || r.done.StopReason != goai.StopReasonToolUse {
		tb.Fatal("final grammar input lost", r.done)
	}
	var generated map[string]any
	if err := json.Unmarshal([]byte(r.json), &generated); err != nil || generated["payload"] != expected {
		tb.Fatal("append-only deltas do not reconstruct input", err)
	}
}
func TestResponsesGrammarInitialAndLargeInputMain(t *testing.T) {
	for _, size := range []int{0, 25600, 256000} {
		body, expected := responsesGrammarFixtureMain(size, "a")
		runResponsesGrammarMain(t, body, expected, "a")
	}
}
func BenchmarkResponsesGrammarGrowthMain(b *testing.B) {
	for _, size := range []int{25600, 256000} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			body, expected := responsesGrammarFixtureMain(size, "")
			b.ReportAllocs()
			b.SetBytes(int64(len(expected)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				runResponsesGrammarMain(b, body, expected, "")
			}
		})
	}
}
