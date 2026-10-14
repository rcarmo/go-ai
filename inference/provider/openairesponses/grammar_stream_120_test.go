package openairesponses

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestResponses120GrammarPropertyAndJSONDeltas(t *testing.T) {
	tool := goai.Tool{Name: "script", Parameters: json.RawMessage(`{"type":"object","required":["code"],"properties":{"code":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
	for _, mode := range []string{"split", "done-only", "item-only", "empty", "non-monotonic"} {
		t.Run(mode, func(t *testing.T) {
			var stream strings.Builder
			emit := func(value map[string]any) {
				bytes, _ := json.Marshal(value)
				stream.WriteString("data: " + string(bytes) + "\n\n")
			}
			emit(map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "custom_tool_call", "id": "ctc_one", "call_id": "call_one", "name": "script"}})
			input := "a\n\"b"
			if mode == "empty" {
				input = ""
			}
			if mode == "split" || mode == "non-monotonic" {
				emit(map[string]any{"type": "response.custom_tool_call_input.delta", "delta": "a\n"})
			}
			if mode == "non-monotonic" {
				input = "changed"
			}
			if mode != "item-only" {
				emit(map[string]any{"type": "response.custom_tool_call_input.done", "input": input})
			}
			emit(map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "custom_tool_call", "id": "ctc_one", "call_id": "call_one", "name": "script", "input": input}})
			emit(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed"}})
			var done *goai.DoneEvent
			var failure *goai.ErrorEvent
			deltas := ""
			processStreamWithOptions(strings.NewReader(stream.String()), &goai.Model{}, nil, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.ToolCallDeltaEvent:
					deltas += e.Delta
				case *goai.DoneEvent:
					done = e
				case *goai.ErrorEvent:
					failure = e
				}
			}, buildResponsesToolViewFromTools([]goai.Tool{tool}, true))
			if mode == "non-monotonic" {
				if failure == nil || done != nil {
					t.Fatal("non-monotonic input accepted", done, failure)
				}
				return
			}
			if failure != nil || done == nil {
				t.Fatal(done, failure)
			}
			args := done.Message.Content[0].Arguments
			if args["code"] != input || len(args) != 1 {
				t.Fatal(args)
			}
			var decoded map[string]string
			if err := json.Unmarshal([]byte(deltas), &decoded); err != nil || decoded["code"] != input || len(decoded) != 1 {
				t.Fatal(deltas, err)
			}
		})
	}
}
