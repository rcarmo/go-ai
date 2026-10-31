package openaicodex

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestCodexInitialFunctionArgumentsBothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		for _, tc := range []struct {
			name, initial, delta, argumentsDone, itemDone string
			want                                          float64
		}{
			{name: "complete", initial: `{"n":1}`, want: 1},
			{name: "prefix", initial: `{"n":`, delta: `1}`, want: 1},
			{name: "arguments-done", initial: `{"n":1`, argumentsDone: `{"n":12}`, want: 12},
			{name: "arguments-correction", initial: `{"n":1}`, argumentsDone: `{"n":2}`, want: 2},
			{name: "item-correction", initial: `{"n":1}`, itemDone: `{"n":3}`, want: 3},
			{name: "empty", itemDone: `{"n":3}`, want: 3},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				var frames []string
				emit := func(event any) {
					data, err := json.Marshal(event)
					if err != nil {
						t.Fatal(err)
					}
					frames = append(frames, string(data))
				}
				item := map[string]any{"type": "function_call", "id": "fc_initial", "call_id": "call_initial", "name": "sample", "arguments": tc.initial}
				emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
				if tc.delta != "" {
					emit(map[string]any{"type": "response.function_call_arguments.delta", "output_index": 0, "delta": tc.delta})
				}
				if tc.argumentsDone != "" {
					emit(map[string]any{"type": "response.function_call_arguments.done", "output_index": 0, "arguments": tc.argumentsDone})
				}
				delete(item, "arguments")
				if tc.itemDone != "" {
					item["arguments"] = tc.itemDone
				}
				emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
				emit(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed"}})
				starts := 0
				var done *goai.Message
				err := streamCodexArgumentFrames(t, transport, frames, func(event goai.Event) {
					data, err := json.Marshal(event)
					if err != nil {
						t.Fatal(err)
					}
					for _, key := range []string{`"partialJson":`, `"customInput":`, `"argumentStream":`} {
						if strings.Contains(string(data), key) {
							t.Fatal("scratch escaped", string(data))
						}
					}
					switch event := event.(type) {
					case *goai.ToolCallStartEvent:
						starts++
						block := event.Partial.Content[0]
						if !block.HasStreamingArguments() || block.Arguments == nil {
							t.Fatal("missing start arguments", block)
						}
						if tc.initial == `{"n":1}` {
							snapshot := goai.SnapshotEvent(event).(*goai.ToolCallStartEvent)
							if snapshot.Partial.Content[0].Arguments["n"] == nil {
								t.Fatal("initial arguments missing", block)
							}
						}
					case *goai.DoneEvent:
						done = event.Message
					case *goai.ErrorEvent:
						t.Fatal("unexpected stream error", event.Err)
					}
				})
				if err != nil || starts != 1 || done == nil || len(done.Content) != 1 {
					t.Fatal("wrong lifecycle", err, starts, done)
				}
				data, err := json.Marshal(done.Content[0].Arguments)
				if err != nil {
					t.Fatal(err)
				}
				var args map[string]any
				if err = json.Unmarshal(data, &args); err != nil {
					t.Fatal(err)
				}
				if args["n"] != tc.want {
					t.Fatal("initial/final arguments lost", args, tc.want)
				}
			})
		}
	}
}

func TestCodexInitialFunctionUTF16BothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"sample","arguments":"{\"s\":\"\ud800\"}"}}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"sample"}}`, `{"type":"response.completed","response":{"status":"completed"}}`}
			starts, done := 0, 0
			check := func(message *goai.Message) {
				value, ok := message.Content[0].Arguments["s"].(goai.UTF16String)
				if !ok {
					t.Fatal("initial UTF16 value replaced", message.Content[0].Arguments)
				}
				units := value.CodeUnits()
				if len(units) != 1 || units[0] != 0xd800 {
					t.Fatal("UTF16 units changed", units)
				}
				if _, err := json.Marshal(message); err != nil {
					t.Fatal(err)
				}
			}
			err := streamCodexArgumentFrames(t, transport, frames, func(event goai.Event) {
				switch event := event.(type) {
				case *goai.ToolCallStartEvent:
					starts++
					check(event.Partial)
				case *goai.DoneEvent:
					done++
					check(event.Message)
				case *goai.ErrorEvent:
					t.Fatal(event.Err)
				}
			})
			if err != nil || starts != 1 || done != 1 {
				t.Fatal("wrong UTF16 lifecycle", err, starts, done)
			}
		})
	}
}
