package openairesponses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOutputSlotInterleavingMain(t *testing.T) {
	for _, transport := range []string{"responses", "azure"} {
		t.Run(transport, func(t *testing.T) {
			encode := func(value any) string {
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return string(raw)
			}
			var frames []string
			add := func(index int, kind, id string) {
				frames = append(frames, encode(map[string]any{"type": "response.output_item.added", "output_index": index, "item": map[string]any{"type": kind, "id": id, "call_id": id, "name": id}}))
			}
			event := func(index int, kind, key string, value any) {
				frames = append(frames, encode(map[string]any{"type": kind, "output_index": index, key: value}))
			}
			add(10, "function_call", "a")
			add(20, "function_call", "b")
			add(30, "message", "text")
			add(40, "reasoning", "reason")
			event(10, "response.function_call_arguments.delta", "delta", `{"x":1}`)
			event(20, "response.function_call_arguments.delta", "delta", `{"low":"`)
			event(40, "response.reasoning_summary_text.delta", "delta", "thought")
			event(30, "response.output_text.delta", "delta", "answer")
			event(999, "response.function_call_arguments.delta", "delta", "corruption")
			event(30, "response.function_call_arguments.delta", "delta", "wrong type")
			event(999, "response.output_item.done", "item", map[string]any{"type": "function_call"})
			event(10, "response.function_call_arguments.done", "arguments", `{"x":2}`)
			event(10, "response.output_item.done", "item", map[string]any{"type": "function_call"})
			event(10, "response.function_call_arguments.delta", "delta", "late corruption")
			event(20, "response.function_call_arguments.done", "arguments", `{"low":"\udfff"}`)
			event(30, "response.output_text.delta", "delta", " tail")
			event(40, "response.reasoning_summary_text.delta", "delta", " tail")
			event(30, "response.output_item.done", "item", map[string]any{"type": "message", "id": "text"})
			event(40, "response.output_item.done", "item", map[string]any{"type": "reasoning", "id": "reason"})
			event(20, "response.output_item.done", "item", map[string]any{"type": "function_call"})
			frames = append(frames, encode(map[string]any{"type": "response.completed", "response": map[string]any{"id": "r", "status": "completed", "usage": map[string]any{}}}))
			var final *goai.Message
			ends := map[int]goai.ToolCall{}
			send := func(event goai.Event) {
				switch e := goai.SnapshotEvent(event).(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ToolCallEndEvent:
					ends[e.ContentIndex] = e.ToolCall
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}
			api := goai.ApiOpenAIResponses
			if transport == "azure" {
				api = goai.ApiAzureOpenAIResponses
			}
			processStream(strings.NewReader("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"), &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api}, send)
			if final == nil || len(final.Content) != 4 || len(ends) != 2 {
				t.Fatal("missing routed blocks", final, ends)
			}
			if ends[0].Name != "a" || ends[0].Arguments["x"] != float64(2) || ends[1].Name != "b" {
				t.Fatal("tool slots crossed", ends)
			}
			low, ok := ends[1].Arguments["low"].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(low.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("second argument stream lost units", ends[1])
			}
			if final.Content[2].Text != "answer tail" || final.Content[3].Thinking != "thought tail" {
				t.Fatal("text/thinking slots crossed", final.Content)
			}
		})
	}
}
