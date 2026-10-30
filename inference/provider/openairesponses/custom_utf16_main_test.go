package openairesponses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestCustomUTF16IndexedInputMain(t *testing.T) {
	for _, transport := range []string{"responses", "azure"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"a","call_id":"a","name":"script","input":"a\ud800"}}`, `{"type":"response.output_item.added","output_index":1,"item":{"type":"custom_tool_call","id":"b","call_id":"b","name":"script"}}`, `{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"\udc00\u0000"}`, `{"type":"response.custom_tool_call_input.delta","output_index":1,"delta":"\udfff"}`, `{"type":"response.custom_tool_call_input.done","output_index":0,"input":"a\ud800\udc00\u0000"}`, `{"type":"response.custom_tool_call_input.done","output_index":1,"input":"\udfff"}`, `{"type":"response.output_item.done","output_index":1,"item":{"type":"custom_tool_call","input":"\udfff"}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call"}}`, `{"type":"response.completed","response":{"status":"completed","usage":{}}}`}
			var final *goai.Message
			deltas := map[int]string{}
			calls := map[int]goai.ToolCall{}
			send := func(event goai.Event) {
				switch e := goai.SnapshotEvent(event).(type) {
				case *goai.ToolCallDeltaEvent:
					deltas[e.ContentIndex] += e.Delta
					if !e.Partial.Content[e.ContentIndex].HasStreamingArguments() {
						t.Error("custom partial lost native ownership")
					}
				case *goai.ToolCallEndEvent:
					calls[e.ContentIndex] = e.ToolCall
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
			if final == nil || len(final.Content) != 2 || len(calls) != 2 {
				t.Fatal("missing custom calls", final, calls)
			}
			if calls[0].Arguments["input"] != "a𐀀\x00" {
				t.Fatal("initial/split input lost", calls[0])
			}
			low, ok := calls[1].Arguments["input"].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(low.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("isolated input lost", calls[1])
			}
			for index, call := range calls {
				if !call.HasStreamingArguments() {
					t.Fatal("final native ownership missing")
				}
				parsed, err := jsonparse.ParseJSONValue(deltas[index], false)
				if err != nil || !reflect.DeepEqual(parsed, call.Arguments) {
					t.Fatal("custom deltas do not reconstruct final arguments", index, parsed, err, deltas[index])
				}
			}
		})
	}
}

func TestCustomFinalReconciliationMain(t *testing.T) {
	for _, transport := range []string{"responses", "azure"} {
		for _, mode := range []string{"empty", "item_only", "non_monotonic", "changed_close", "append_closed"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				frames := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"a","call_id":"a","name":"script"}}`}
				if mode != "empty" && mode != "item_only" {
					frames = append(frames, `{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"abc"}`)
				}
				if mode == "non_monotonic" {
					frames = append(frames, `{"type":"response.custom_tool_call_input.done","output_index":0,"input":""}`)
				} else if mode != "item_only" {
					input := "abc"
					if mode == "empty" {
						input = ""
					}
					raw, _ := json.Marshal(map[string]any{"type": "response.custom_tool_call_input.done", "output_index": 0, "input": input})
					frames = append(frames, string(raw))
				}
				if mode == "changed_close" {
					frames = append(frames, `{"type":"response.custom_tool_call_input.done","output_index":0,"input":"changed"}`)
				}
				if mode == "append_closed" {
					frames = append(frames, `{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"tail"}`)
				}
				item := `{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call"}}`
				if mode == "item_only" {
					item = `{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","input":"\udfff"}}`
				}
				frames = append(frames, item, `{"type":"response.completed","response":{"status":"completed","usage":{}}}`)
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
				if mode == "empty" || mode == "item_only" {
					if done != 1 || failures != 0 {
						t.Fatal("valid custom close failed", done, failures)
					}
				} else if done != 0 || failures != 1 {
					t.Fatal("invalid custom reconciliation accepted", done, failures)
				}
			})
		}
	}
}

func TestResponsesCustomNativeReplayMain(t *testing.T) {
	value, err := jsonparse.ParseJSONValue(`"\udfff"`, true)
	if err != nil {
		t.Fatal(err)
	}
	block := goai.ContentBlock{Type: "toolCall", ID: "c|ctc_i", Name: "script"}
	block.SetStreamingArguments(map[string]any{"input": value}, "")
	tool := goai.Tool{Name: "script", Parameters: json.RawMessage(`{"type":"object","required":["input"],"properties":{"input":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
	items := buildAssistantItemsWithToolView(0, goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{block}}, &goai.Model{}, buildResponsesToolViewFromTools([]goai.Tool{tool}, true))
	if len(items) != 1 {
		t.Fatal(items)
	}
	item := items[0].(map[string]interface{})
	if item["type"] != "custom_tool_call" || item["input"] != "" {
		t.Fatal("native scalar replay did not retain existing sanitization", item)
	}
}

func TestResponsesEmptyGrammarPropertyMain(t *testing.T) {
	tool := goai.Tool{Name: "script", Parameters: json.RawMessage(`{"type":"object","required":[""],"properties":{"":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
	view := buildResponsesToolViewFromTools([]goai.Tool{tool}, true)
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		t.Run(string(api), func(t *testing.T) {
			source := "data: " + strings.Join([]string{`{"type":"response.output_item.added","item":{"type":"custom_tool_call","id":"ctc_i","call_id":"c","name":"script"}}`, `{"type":"response.custom_tool_call_input.done","input":"literal"}`, `{"type":"response.output_item.done","item":{"type":"custom_tool_call","input":"literal"}}`, `{"type":"response.completed","response":{"status":"completed"}}`}, "\n\ndata: ") + "\n\n"
			model := &goai.Model{Api: api, Provider: goai.ProviderOpenAI}
			var final *goai.Message
			delta := ""
			processStreamWithOptions(strings.NewReader(source), model, nil, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ToolCallDeltaEvent:
					delta += e.Delta
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}, view)
			if final == nil {
				t.Fatal("missing completion")
			}
			args := final.Content[0].Arguments
			if len(args) != 1 || args[""] != "literal" || delta != `{"":"literal"}` {
				t.Fatal("empty property lost", args, delta)
			}
			items := buildAssistantItemsWithToolView(0, *final, model, view)
			if len(items) != 1 {
				t.Fatal(items)
			}
			item := items[0].(map[string]interface{})
			if item["type"] != "custom_tool_call" || item["input"] != "literal" || item["id"] != "ctc_i" {
				t.Fatal("empty-property replay lost", item)
			}
		})
	}
}
