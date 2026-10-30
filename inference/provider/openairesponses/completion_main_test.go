package openairesponses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestDoneOnlyItemsMain(t *testing.T) {
	for _, transport := range []string{"responses", "azure"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{`{"type":"response.output_item.done","output_index":8,"item":{"type":"function_call","id":"i","call_id":"c","name":"leaf","arguments":"{\"low\":\"\udfff\"}"}}`, `{"type":"response.output_item.done","output_index":9,"item":{"type":"message","id":"m","content":[{"type":"output_text","text":"answer"},{"type":"refusal","refusal":" refusal"}]}}`, `{"type":"response.output_item.done","output_index":10,"item":{"type":"reasoning","id":"r","summary":[{"text":"one"},{"text":"two"}]}}`, `{"type":"response.completed","response":{"status":"completed","usage":{}}}`}
			starts, ends := 0, 0
			var final *goai.Message
			send := func(event goai.Event) {
				switch e := event.(type) {
				case *goai.ToolCallStartEvent, *goai.TextStartEvent, *goai.ThinkingStartEvent:
					starts++
				case *goai.ToolCallEndEvent, *goai.TextEndEvent, *goai.ThinkingEndEvent:
					ends++
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}
			api := goai.ApiOpenAIResponses
			if transport == "azure" {
				api = goai.ApiAzureOpenAIResponses
			}
			processStream(strings.NewReader("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"), &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api}, send)
			if final == nil || len(final.Content) != 3 || starts != 3 || ends != 3 {
				t.Fatal("done-only reconstruction incomplete", final, starts, ends)
			}
			low, ok := final.Content[0].Arguments["low"].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(low.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("recovered argument units lost")
			}
			if final.Content[1].Text != "answer refusal" || final.Content[2].Thinking != "one\n\ntwo" {
				t.Fatal("final text projection", final.Content)
			}
		})
	}
}

func TestUnfinishedToolCompletionMain(t *testing.T) {
	for _, transport := range []string{"responses", "azure"} {
		for _, mode := range []string{"indexed", "legacy_overwrite", "wrong_kind", "arguments_only", "partial_end", "length"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				encode := func(v any) string {
					raw, err := json.Marshal(v)
					if err != nil {
						t.Fatal(err)
					}
					return string(raw)
				}
				add := func(index int) string {
					value := map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "function_call", "id": "i", "call_id": "c", "name": "leaf"}}
					if mode != "legacy_overwrite" {
						value["output_index"] = index
					}
					return encode(value)
				}
				frames := []string{add(0)}
				switch mode {
				case "legacy_overwrite", "partial_end":
					frames = append(frames, add(1))
				case "wrong_kind":
					frames = append(frames, `{"type":"response.output_item.done","output_index":0,"item":{"type":"message"}}`)
				case "arguments_only":
					frames = append(frames, `{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{}"}`)
				}
				if mode == "partial_end" {
					frames = append(frames, `{"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","arguments":"{}"}}`)
				}
				status := "completed"
				if mode == "length" {
					status = "incomplete"
				}
				response := map[string]any{"status": status, "usage": map[string]any{}}
				if mode == "length" {
					response["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
				}
				frames = append(frames, encode(map[string]any{"type": "response.completed", "response": response}))
				done, failures := 0, 0
				send := func(event goai.Event) {
					switch event.(type) {
					case *goai.DoneEvent:
						done++
					case *goai.ErrorEvent:
						failures++
					}
				}
				api := goai.ApiOpenAIResponses
				if transport == "azure" {
					api = goai.ApiAzureOpenAIResponses
				}
				processStream(strings.NewReader("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"), &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api}, send)
				if mode == "length" {
					if done != 1 || failures != 0 {
						t.Fatal("length result changed", done, failures)
					}
				} else if done != 0 || failures != 1 {
					t.Fatal("unfinished call published", done, failures)
				}
			})
		}
	}
}
