package mistral

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func mistralSnapshotMain(t *testing.T, m *goai.Message) []map[string]any {
	t.Helper()
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Content []map[string]any `json:"content"`
	}
	if err = json.Unmarshal(data, &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, block := range snapshot.Content {
		for _, key := range []string{"partialArgs", "argsBuf", "argumentsStream", "index"} {
			if _, ok := block[key]; ok {
				t.Fatal("scratch escaped", key, string(data))
			}
		}
	}
	return snapshot.Content
}

func TestMistralArgumentPreviewMain(t *testing.T) {
	var wire strings.Builder
	emit := func(delta map[string]any, finish any) {
		data, err := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta, "finish_reason": finish}}})
		if err != nil {
			t.Fatal(err)
		}
		wire.WriteString("data: " + string(data) + "\n\n")
	}
	tool := func(index int, id, name, arguments string) map[string]any {
		return map[string]any{"index": index, "id": id, "function": map[string]any{"name": name, "arguments": arguments}}
	}
	emit(map[string]any{"tool_calls": []any{tool(0, "one", "first", `{"n":1`), tool(1, "two", "second", `{"s":"v`)}}, nil)
	emit(map[string]any{"tool_calls": []any{tool(0, "", "", `2}`), tool(1, "", "", `alue"}`)}}, nil)
	emit(map[string]any{}, "tool_calls")
	starts, deltas, ends := 0, 0, 0
	var terminal *goai.Message
	processSSEStream(strings.NewReader(wire.String()), &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, func(event goai.Event) {
		switch event := event.(type) {
		case *goai.ToolCallStartEvent:
			starts++
			blocks := mistralSnapshotMain(t, event.Partial)
			if _, ok := blocks[event.ContentIndex]["arguments"].(map[string]any); !ok {
				t.Fatal("start arguments omitted", blocks)
			}
		case *goai.ToolCallDeltaEvent:
			deltas++
			blocks := mistralSnapshotMain(t, event.Partial)
			args := blocks[event.ContentIndex]["arguments"].(map[string]any)
			wantKey, wantValue := "n", any(float64(1))
			if event.ContentIndex == 1 {
				wantKey, wantValue = "s", "v"
			}
			if deltas > 2 {
				if event.ContentIndex == 0 {
					wantValue = float64(12)
				} else {
					wantValue = "value"
				}
			}
			if args[wantKey] != wantValue {
				t.Fatal("partial arguments not updated", args, wantKey, wantValue)
			}
		case *goai.ToolCallEndEvent:
			ends++
			if !event.ToolCall.HasStreamingArguments() {
				t.Fatal("end lost argument domain")
			}
			mistralSnapshotMain(t, event.Partial)
		case *goai.DoneEvent:
			terminal = event.Message
		case *goai.ErrorEvent:
			t.Fatal(event.Err)
		}
	})
	if starts != 2 || deltas != 4 || ends != 2 || terminal == nil || terminal.StopReason != goai.StopReasonToolUse {
		t.Fatal("wrong lifecycle", starts, deltas, ends, terminal)
	}
	blocks := mistralSnapshotMain(t, terminal)
	if blocks[0]["id"] != "one" || blocks[1]["id"] != "two" || blocks[0]["arguments"].(map[string]any)["n"] != float64(12) || blocks[1]["arguments"].(map[string]any)["s"] != "value" {
		t.Fatal("tool streams mixed", blocks)
	}
}

func TestMistralArgumentUTF16AndDiagnosticMain(t *testing.T) {
	for _, tc := range []struct {
		name, arguments string
		bad             bool
	}{{name: "utf16", arguments: `{\"s\":\"\ud800\"}`}, {name: "invalid", arguments: `{\"s\":`, bad: true}} {
		t.Run(tc.name, func(t *testing.T) {
			wire := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"one","function":{"name":"sample","arguments":"` + tc.arguments + `"}}]},"finish_reason":null}]}` + "\n\n" + `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n"
			var done *goai.Message
			previews := 0
			processSSEStream(strings.NewReader(wire), &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, func(event goai.Event) {
				switch event := event.(type) {
				case *goai.ToolCallDeltaEvent:
					previews++
					mistralSnapshotMain(t, event.Partial)
					if !tc.bad {
						value, ok := event.Partial.Content[0].Arguments["s"].(goai.UTF16String)
						if !ok || len(value.CodeUnits()) != 1 || value.CodeUnits()[0] != 0xd800 {
							t.Fatal("UTF16 preview changed", event.Partial.Content[0].Arguments)
						}
					}
				case *goai.DoneEvent:
					done = event.Message
				case *goai.ErrorEvent:
					t.Fatal(event.Err)
				}
			})
			if done == nil || previews != 1 || len(done.Content) != 1 {
				t.Fatal("missing terminal", done, previews)
			}
			mistralSnapshotMain(t, done)
			block := done.Content[0]
			if tc.bad {
				if block.ArgumentsError == "" || len(block.Arguments) != 0 {
					t.Fatal("invalid arguments lost diagnostic", block)
				}
			} else {
				value, ok := block.Arguments["s"].(goai.UTF16String)
				if !ok || len(value.CodeUnits()) != 1 || value.CodeUnits()[0] != 0xd800 {
					t.Fatal("UTF16 final changed", block.Arguments)
				}
			}
		})
	}
}

func TestMistralArgumentErrorPartialMain(t *testing.T) {
	wire := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"one","function":{"name":"sample","arguments":"{\"n\":1}"}}]}}]}` + "\n\n"
	failures := 0
	processSSEStream(strings.NewReader(wire), &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, func(event goai.Event) {
		switch event := event.(type) {
		case *goai.ErrorEvent:
			failures++
			blocks := mistralSnapshotMain(t, event.Error)
			if blocks[0]["arguments"].(map[string]any)["n"] != float64(1) {
				t.Fatal("error lost partial arguments", blocks)
			}
		case *goai.DoneEvent:
			t.Fatal("missing finish reason accepted")
		}
	})
	if failures != 1 {
		t.Fatal("missing error", failures)
	}
}
