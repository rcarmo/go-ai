package openai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestOpenAIGrammarUTF16InterleavingMain(t *testing.T) {
	source := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"a","type":"custom","custom":{"name":"python","input":"a\ud800"}}]}}]}` + "\n\n" + `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"b","function":{"name":"leaf","arguments":"{\"x\":"}}]}}]}` + "\n\n" + `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"type":"custom","custom":{"input":"\udc00\u0000"}},{"index":2,"id":"c","type":"custom","custom":{"name":"python","input":"\udfff"}}]}}]}` + "\n\n" + `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"1}"}}]},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
	model := &goai.Model{ID: "test", Api: goai.ApiOpenAICompletions, Provider: goai.ProviderOpenAI}
	var final *goai.Message
	deltas := map[int]string{}
	calls := map[int]goai.ToolCall{}
	processSSEStreamWithOptions(strings.NewReader(source), model, nil, func(event goai.Event) {
		switch e := goai.SnapshotEvent(event).(type) {
		case *goai.ToolCallDeltaEvent:
			deltas[e.ContentIndex] += e.Delta
		case *goai.ToolCallEndEvent:
			calls[e.ContentIndex] = e.ToolCall
		case *goai.DoneEvent:
			final = e.Message
		case *goai.ErrorEvent:
			t.Fatal(e.Err)
		}
	}, []goai.Tool{grammarUTF16CodeTool()})
	if final == nil || len(final.Content) != 3 || len(calls) != 3 {
		t.Fatal("missing interleaved calls", final, calls)
	}
	if final.Content[0].Arguments["code"] != "a𐀀\x00" || final.Content[1].Arguments["x"] != float64(1) {
		t.Fatal("interleaved values lost", final.Content)
	}
	low, ok := final.Content[2].Arguments["code"].(goai.UTF16String)
	if !ok || !reflect.DeepEqual(low.CodeUnits(), []uint16{0xdfff}) {
		t.Fatal("isolated literal input lost", final.Content[2])
	}
	for index, call := range calls {
		if !call.HasStreamingArguments() || !final.Content[index].HasStreamingArguments() {
			t.Fatal("custom/native argument ownership lost", index)
		}
		stream := jsonparse.ToolCallArgumentsStream{}
		stream.Append(deltas[index])
		args, diagnostic := stream.Finish()
		if diagnostic != "" {
			t.Fatal("invalid grammar JSON deltas", diagnostic)
		}
		if !reflect.DeepEqual(args, call.Arguments) {
			t.Fatal("delta arguments differ from final", index, args, call.Arguments)
		}
	}
	yes := true
	compat := &goai.OpenAICompletionsCompat{SupportsOpenAIGrammarTools: &yes}
	messages := convertMessages(model, &goai.Context{Messages: []goai.Message{*final}}, compat, []goai.Tool{grammarUTF16CodeTool()})
	found := false
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.ID == "c" {
				found = true
				if call.Type != "custom" || call.Custom["input"] != "" {
					t.Fatal("native custom replay bypassed existing sanitization", call)
				}
			}
		}
	}
	if !found {
		t.Fatal("isolated custom replay missing")
	}
	compat.SupportsOpenAIGrammarTools = nil
	fallback := convertMessages(model, &goai.Context{Messages: []goai.Message{*final}}, compat, []goai.Tool{grammarUTF16CodeTool()})
	found = false
	for _, message := range fallback {
		for _, call := range message.ToolCalls {
			if call.ID == "c" {
				found = true
				args, err := jsonparse.ParseJSONValue(call.Function.Arguments, true)
				if err != nil {
					t.Fatal(err)
				}
				value, ok := args.(map[string]any)["code"].(goai.UTF16String)
				if !ok || !reflect.DeepEqual(value.CodeUnits(), []uint16{0xdfff}) {
					t.Fatal("function fallback lost native input", args)
				}
			}
		}
	}
	if !found {
		t.Fatal("function fallback missing")
	}
}

func TestGrammarNativeInputOwnershipMain(t *testing.T) {
	value, err := jsonparse.ParseJSONValue(`"\udfff"`, true)
	if err != nil {
		t.Fatal(err)
	}
	block := goai.ContentBlock{Type: "toolCall", Arguments: map[string]any{"code": value}}
	if _, ok := customGrammarInput(block, "code"); ok {
		t.Fatal("unmarked native scalar accepted as grammar input")
	}
	block.SetStreamingArguments(block.Arguments, "")
	text, ok := customGrammarInput(block, "code")
	if !ok || text != value.(goai.UTF16String).WTF8() {
		t.Fatal("marked grammar input lost")
	}
}

func grammarUTF16CodeTool() goai.Tool {
	return goai.Tool{Name: "python", Parameters: json.RawMessage(`{"type":"object","required":["code"],"properties":{"code":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
}

func TestOpenAIGrammarEmptyCloseSnapshotMain(t *testing.T) {
	source := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"a\",\"type\":\"custom\",\"custom\":{\"name\":\"python\",\"input\":\"\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
	deltas := 0
	processSSEStreamWithOptions(strings.NewReader(source), &goai.Model{}, nil, func(event goai.Event) {
		if e, ok := event.(*goai.ToolCallDeltaEvent); ok {
			deltas++
			block := e.Partial.Content[e.ContentIndex]
			if !block.HasStreamingArguments() || block.Arguments["code"] != "" {
				t.Error("empty closing delta lost argument ownership", block)
			}
		}
		if e, ok := event.(*goai.ErrorEvent); ok {
			t.Error(e.Err)
		}
	}, []goai.Tool{grammarUTF16CodeTool()})
	if deltas != 1 {
		t.Fatal("missing empty closure delta", deltas)
	}
}

func TestOpenAIEmptyGrammarPropertyMain(t *testing.T) {
	tool := grammarUTF16CodeTool()
	tool.Parameters = json.RawMessage(`{"type":"object","required":[""],"properties":{"":{"type":"string"}}}`)
	property, selected := grammarInputPropertyFor("python", [][]goai.Tool{{tool}})
	if !selected || property != "" {
		t.Fatal("empty property lost", property, selected)
	}
	if _, selected = grammarInputPropertyFor("unknown", [][]goai.Tool{{tool}}); selected {
		t.Fatal("unknown selected")
	}
	for _, input := range []string{"", "literal"} {
		t.Run(input, func(t *testing.T) {
			frame, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "a", "type": "custom", "custom": map[string]any{"name": "python", "input": input}}}}, "finish_reason": "tool_calls"}}})
			var final *goai.Message
			delta := ""
			processSSEStreamWithOptions(strings.NewReader("data: "+string(frame)+"\n\ndata: [DONE]\n\n"), &goai.Model{}, nil, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ToolCallDeltaEvent:
					delta += e.Delta
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}, []goai.Tool{tool})
			if final == nil {
				t.Fatal("missing completion")
			}
			args := final.Content[0].Arguments
			if len(args) != 1 || args[""] != input {
				t.Fatal("empty property defaulted", args)
			}
			parsed, err := jsonparse.ParseJSONValue(delta, false)
			if err != nil || !reflect.DeepEqual(parsed, args) {
				t.Fatal("delta mismatch", parsed, err)
			}
			yes := true
			messages := convertMessages(&goai.Model{}, &goai.Context{Messages: []goai.Message{*final}}, &goai.OpenAICompletionsCompat{SupportsOpenAIGrammarTools: &yes}, []goai.Tool{tool})
			found := false
			for _, message := range messages {
				for _, call := range message.ToolCalls {
					if call.ID == "a" {
						found = true
						if call.Type != "custom" || call.Custom["input"] != input {
							t.Fatal("empty-property replay fell back", call)
						}
					}
				}
			}
			if !found {
				t.Fatal("missing replay")
			}
		})
	}
}
