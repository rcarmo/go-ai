package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func assertResponsesScratchAbsentMain(t *testing.T, message *goai.Message) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]any
	if err = json.Unmarshal(encoded, &snapshot); err != nil {
		t.Fatal(err)
	}
	if content, ok := snapshot["content"].([]any); ok {
		for _, value := range content {
			block, ok := value.(map[string]any)
			if !ok {
				t.Fatal("invalid content block")
			}
			for _, key := range []string{"index", "partialJson", "customInput", "argumentStream"} {
				if _, exists := block[key]; exists {
					t.Fatal("stream scratch persisted", key, string(encoded))
				}
			}
		}
	}
	return snapshot
}

func TestResponsesInitialFunctionArgumentsMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		for _, tc := range []struct {
			name, initial, delta, argumentsDone, itemDone, terminal string
			want                                                    float64
			unfinished                                              bool
		}{
			{name: "initial-complete", initial: `{"n":1}`, want: 1},
			{name: "initial-prefix", initial: `{"n":`, delta: `1}`, want: 1},
			{name: "arguments-done", initial: `{"n":1`, argumentsDone: `{"n":12}`, want: 12},
			{name: "arguments-correction", initial: `{"n":1}`, argumentsDone: `{"n":2}`, want: 2},
			{name: "item-correction", initial: `{"n":1}`, itemDone: `{"n":3}`, want: 3},
			{name: "empty-initial", itemDone: `{"n":3}`, want: 3},
			{name: "unfinished", initial: `{"n":1}`, unfinished: true},
			{name: "provider-error", initial: `{"n":1}`, unfinished: true, terminal: "response.failed"},
			{name: "eof", initial: `{"n":1}`, unfinished: true, terminal: "eof"},
		} {
			t.Run(string(api)+"/"+tc.name, func(t *testing.T) {
				var wire strings.Builder
				emit := func(event any) {
					data, err := json.Marshal(event)
					if err != nil {
						t.Fatal(err)
					}
					wire.WriteString("data: " + string(data) + "\n\n")
				}
				item := map[string]any{"type": "function_call", "id": "fc_initial", "call_id": "call_initial", "name": "sample", "arguments": tc.initial}
				emit(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
				if tc.delta != "" {
					emit(map[string]any{"type": "response.function_call_arguments.delta", "output_index": 0, "delta": tc.delta})
				}
				if tc.argumentsDone != "" {
					emit(map[string]any{"type": "response.function_call_arguments.done", "output_index": 0, "arguments": tc.argumentsDone})
				}
				if !tc.unfinished {
					delete(item, "arguments")
					if tc.itemDone != "" {
						item["arguments"] = tc.itemDone
					}
					emit(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
				}
				if tc.terminal != "eof" {
					kind := tc.terminal
					if kind == "" {
						kind = "response.completed"
					}
					emit(map[string]any{"type": kind, "response": map[string]any{"status": map[string]string{"response.completed": "completed", "response.failed": "failed"}[kind], "error": map[string]any{"message": "fixture provider failure"}}})
				}
				model := &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api}
				var terminal *goai.Message
				starts, ends, failures := 0, 0, 0
				processStream(strings.NewReader(wire.String()), model, func(event goai.Event) {
					switch event := event.(type) {
					case *goai.StartEvent:
						assertResponsesScratchAbsentMain(t, event.Partial)
					case *goai.ToolCallStartEvent:
						starts++
						snapshot := assertResponsesScratchAbsentMain(t, event.Partial)
						content := snapshot["content"].([]any)
						block := content[0].(map[string]any)
						if _, ok := block["arguments"].(map[string]any); !ok {
							t.Fatal("initial arguments must be an object", block)
						}
						if tc.initial == `{"n":1}` {
							if block["arguments"].(map[string]any)["n"] != float64(1) {
								t.Fatal("initial arguments unavailable at start", block)
							}
						}
					case *goai.ToolCallDeltaEvent:
						assertResponsesScratchAbsentMain(t, event.Partial)
					case *goai.ToolCallEndEvent:
						ends++
						assertResponsesScratchAbsentMain(t, event.Partial)
					case *goai.DoneEvent:
						terminal = event.Message
						assertResponsesScratchAbsentMain(t, event.Message)
					case *goai.ErrorEvent:
						failures++
						assertResponsesScratchAbsentMain(t, event.Error)
					}
				})
				if starts != 1 {
					t.Fatal("wrong start count", starts)
				}
				if tc.unfinished {
					if failures != 1 || terminal != nil || ends != 0 {
						t.Fatal("unfinished call escaped", failures, terminal, ends)
					}
					return
				}
				if failures != 0 || ends != 1 || terminal == nil || len(terminal.Content) != 1 {
					t.Fatal("wrong terminal lifecycle", failures, ends, terminal)
				}
				snapshot := assertResponsesScratchAbsentMain(t, terminal)
				args := snapshot["content"].([]any)[0].(map[string]any)["arguments"].(map[string]any)
				if args["n"] != tc.want {
					t.Fatal("initial/final arguments lost", args, tc.want)
				}
			})
		}
	}
}

func TestResponsesInitialFunctionUTF16Main(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		t.Run(string(api), func(t *testing.T) {
			wire := `data: {"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"sample","arguments":"{\"s\":\"\ud800\"}"}}

data: {"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"fc","call_id":"call","name":"sample"}}

data: {"type":"response.completed","response":{"status":"completed"}}

`
			starts, done := 0, 0
			check := func(message *goai.Message) {
				assertResponsesScratchAbsentMain(t, message)
				value, ok := message.Content[0].Arguments["s"].(goai.UTF16String)
				if !ok {
					t.Fatal("initial UTF16 value replaced", message.Content[0].Arguments)
				}
				units := value.CodeUnits()
				if len(units) != 1 || units[0] != 0xd800 {
					t.Fatal("UTF16 units changed", units)
				}
			}
			processStream(strings.NewReader(wire), &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api}, func(event goai.Event) {
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
			if starts != 1 || done != 1 {
				t.Fatal("wrong UTF16 lifecycle", starts, done)
			}
		})
	}
}
