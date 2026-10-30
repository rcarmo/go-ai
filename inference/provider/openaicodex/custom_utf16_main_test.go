package openaicodex

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestCustomUTF16IndexedInputMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
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
			runCodexArgumentFrames(t, transport, frames, send)
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
	for _, transport := range []string{"sse", "websocket"} {
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
				session := t.Name()
				defer CloseOpenAICodexWebSocketSessions(session)
				err := streamCodexArgumentFrames(t, transport, frames, send, session)
				if err != nil {
					if transport != "websocket" || mode == "empty" || mode == "item_only" || !isCodexNonTransportError(err) {
						t.Fatal(err)
					}
					failures++
				}
				if failures > 0 && transport == "websocket" {
					codexWebSocketSessionsMu.Lock()
					cached := cachedCodexSessionForTest(session) != nil
					codexWebSocketSessionsMu.Unlock()
					if cached {
						t.Fatal("bad custom input retained cached connection")
					}
				}
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
