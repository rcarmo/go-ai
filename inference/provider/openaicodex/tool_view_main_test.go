package openaicodex

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func codexPayloadTool(property string) goai.Tool {
	raw, _ := json.Marshal(map[string]any{"type": "object", "required": []string{property}, "properties": map[string]any{property: map[string]any{"type": "string"}}})
	return goai.Tool{Name: "script", Parameters: raw, ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}
}
func codexGrammarModel() *goai.Model {
	yes := true
	return &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsOpenAIGrammarTools: &yes}}
}

func TestCodexSchemaPropertyStreamReplayMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_i","call_id":"c","name":"script","input":"a"}}`, `{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"\udfff"}`, `{"type":"response.custom_tool_call_input.done","output_index":0,"input":"a\udfff"}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","input":"a\udfff"}}`, `{"type":"response.completed","response":{"id":"r","status":"completed","usage":{}}}`}
			tool := codexPayloadTool("payload")
			session := t.Name()
			defer CloseOpenAICodexWebSocketSessions(session)
			var final *goai.Message
			delta := ""
			send := func(event goai.Event) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ToolCallDeltaEvent:
					delta += e.Delta
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			}
			if err := streamCodexArgumentFramesWithTools(t, transport, frames, []goai.Tool{tool}, send, session); err != nil {
				t.Fatal(err)
			}
			if final == nil {
				t.Fatal("missing completion")
			}
			args := final.Content[0].Arguments
			if len(args) != 1 {
				t.Fatal(args)
			}
			value, ok := args["payload"].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(value.CodeUnits(), []uint16{'a', 0xdfff}) {
				t.Fatal("schema-mapped input lost", args)
			}
			parsed, err := jsonparse.ParseJSONValue(delta, false)
			if err != nil || !reflect.DeepEqual(parsed, args) {
				t.Fatal("JSON delta property mismatch", parsed, err)
			}
			model := codexGrammarModel()
			input := &goai.Context{Tools: []goai.Tool{tool}, Messages: []goai.Message{*final}}
			req := buildCodexRequest(model, input, nil)
			var items []map[string]any
			if err = json.Unmarshal(req.Input, &items); err != nil {
				t.Fatal(err)
			}
			if len(items) < 1 || items[0]["type"] != "custom_tool_call" || items[0]["input"] != "a" || items[0]["id"] != "ctc_i" {
				t.Fatal("custom replay type/input/id", items)
			}
			if transport == "websocket" {
				codexWebSocketSessionsMu.Lock()
				entry := cachedCodexSessionForTest(session)
				codexWebSocketSessionsMu.Unlock()
				if entry == nil {
					t.Fatal("missing cached continuation")
				}
				entry.mu.Lock()
				defer entry.mu.Unlock()
				if entry.continuation == nil || len(entry.continuation.lastResponseItems) != 1 {
					t.Fatal("missing cached output")
				}
				item := entry.continuation.lastResponseItems[0].(map[string]interface{})
				if item["type"] != "custom_tool_call" || item["input"] != "a" {
					t.Fatal("cached output lost tool view", item)
				}
			}
			model.ResponsesCompat.SupportsOpenAIGrammarTools = nil
			req = buildCodexRequest(model, input, nil)
			if err = json.Unmarshal(req.Input, &items); err != nil {
				t.Fatal(err)
			}
			if items[0]["type"] != "function_call" {
				t.Fatal("unsupported grammar did not fall back", items)
			}
			fallback, err := jsonparse.ParseJSONValue(items[0]["arguments"].(string), true)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := fallback.(map[string]any)["payload"].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(got.CodeUnits(), value.CodeUnits()) {
				t.Fatal("function fallback lost units", fallback)
			}
		})
	}
}

func TestCodexHistoricalCustomPropertyMain(t *testing.T) {
	model := codexGrammarModel()
	old := codexPayloadTool("old")
	latest := codexPayloadTool("renamed")
	block := goai.ContentBlock{Type: "toolCall", ID: "c|ctc_i", Name: "script", Arguments: map[string]any{"renamed": "value"}}
	input := &goai.Context{Tools: []goai.Tool{old}, Messages: []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{old}}, {Role: goai.RoleSystem, ToolsAdded: []goai.Tool{latest}}, {Role: goai.RoleAssistant, Content: []goai.ContentBlock{block}}}}
	view, err := newCodexToolView(model, input)
	if err != nil || view.inputProperty("script") != "renamed" {
		t.Fatal("latest declaration did not win", view, err)
	}
	input.Tools = nil
	req := buildCodexRequest(model, input, nil)
	var items []map[string]any
	if err = json.Unmarshal(req.Input, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) < 1 || items[0]["type"] != "custom_tool_call" || items[0]["input"] != "value" {
		t.Fatal("removed declaration lost replay mapping", items)
	}
	generic := latest
	generic.ConstrainedSampling = nil
	input.Messages = append(input.Messages, goai.Message{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{generic}})
	view, err = newCodexToolView(model, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, selected := view.properties["script"]; selected {
		t.Fatal("latest generic declaration retained custom mapping")
	}
}

func TestCodexCustomPreflightMain(t *testing.T) {
	model := codexGrammarModel()
	model.BaseURL = "http://127.0.0.1:1"
	bad := codexPayloadTool("payload")
	bad.Parameters = json.RawMessage(`{"type":"object","required":["payload"],"properties":{"payload":{"type":"number"}}}`)
	contexts := []*goai.Context{{Tools: []goai.Tool{bad}}, {Tools: []goai.Tool{codexPayloadTool("payload")}, Messages: []goai.Message{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "toolCall", Name: "script", Arguments: map[string]any{"payload": 42}}}}}}}
	for _, input := range contexts {
		err := streamViaWebSocket(context.Background(), model, input, nil, "fixture", func(goai.Event) {})
		if err == nil || !isCodexNonTransportError(err) || !strings.Contains(err.Error(), "grammar") {
			t.Fatal("WebSocket malformed input escaped preflight", err)
		}
		failures := 0
		streamViaSSE(context.Background(), model, input, nil, "fixture", func(event goai.Event) {
			if e, ok := event.(*goai.ErrorEvent); ok {
				failures++
				if !strings.Contains(e.Err.Error(), "grammar") {
					t.Error(e.Err)
				}
			}
		}, nil)
		if failures != 1 {
			t.Fatal("SSE malformed input escaped preflight", failures)
		}
	}
}

func TestCodexNativeFallbackKeysMain(t *testing.T) {
	keyValue, err := jsonparse.ParseJSONValue(`"\ud800"`, true)
	if err != nil {
		t.Fatal(err)
	}
	low, err := jsonparse.ParseJSONValue(`"\udfff"`, true)
	if err != nil {
		t.Fatal(err)
	}
	key := keyValue.(goai.UTF16String).WTF8()
	block := goai.ContentBlock{Type: "toolCall", ID: "c", Name: "leaf"}
	block.SetStreamingArguments(map[string]any{key: low}, "")
	items := buildCodexAssistantItems(goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{block}}, codexGrammarModel())
	if len(items) != 1 {
		t.Fatal(items)
	}
	args, err := jsonparse.ParseJSONValue(items[0].(map[string]interface{})["arguments"].(string), true)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := args.(map[string]any)[key].(goai.UTF16String)
	if !ok || !reflect.DeepEqual(value.CodeUnits(), []uint16{0xdfff}) {
		t.Fatal("native fallback key/value normalized", args)
	}
}

func TestCodexEmptySchemaPropertyMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			tool := codexPayloadTool("")
			frames := []string{`{"type":"response.output_item.added","item":{"type":"custom_tool_call","name":"script","id":"ctc_i","call_id":"c"}}`, `{"type":"response.custom_tool_call_input.done","input":"literal"}`, `{"type":"response.output_item.done","item":{"type":"custom_tool_call","input":"literal"}}`, `{"type":"response.completed","response":{"status":"completed","usage":{}}}`}
			var final *goai.Message
			delta := ""
			err := streamCodexArgumentFramesWithTools(t, transport, frames, []goai.Tool{tool}, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ToolCallDeltaEvent:
					delta += e.Delta
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			})
			if err != nil || final == nil {
				t.Fatal(final, err)
			}
			args := final.Content[0].Arguments
			if len(args) != 1 || args[""] != "literal" {
				t.Fatal("empty property became input", args)
			}
			if delta != `{"":"literal"}` {
				t.Fatal("wrong JSON property", delta)
			}
			req := buildCodexRequest(codexGrammarModel(), &goai.Context{Tools: []goai.Tool{tool}, Messages: []goai.Message{*final}}, nil)
			var items []map[string]any
			if err = json.Unmarshal(req.Input, &items); err != nil {
				t.Fatal(err)
			}
			if len(items) == 0 || items[0]["type"] != "custom_tool_call" || items[0]["input"] != "literal" {
				t.Fatal("empty-property replay changed", items)
			}
		})
	}
}

func TestCodexGrammarVariantsMain(t *testing.T) {
	for _, tc := range []struct{ name, lark, regex, syntax, definition string }{{"lark", "  start: /.+/\n", "  .+  ", "lark", "  start: /.+/\n"}, {"regex", " \n\t", "  .+\n", "regex", "  .+\n"}} {
		t.Run(tc.name, func(t *testing.T) {
			tool := codexPayloadTool("payload")
			tool.ConstrainedSampling.Variants = map[string]string{"openai_lark": tc.lark, "openai_regex": tc.regex}
			input := &goai.Context{Tools: []goai.Tool{tool}}
			if _, err := newCodexToolView(codexGrammarModel(), input); err != nil {
				t.Fatal(err)
			}
			req := buildCodexRequest(codexGrammarModel(), input, nil)
			if len(req.Tools) != 1 || req.Tools[0].Format["syntax"] != tc.syntax || req.Tools[0].Format["definition"] != tc.definition {
				t.Fatal("variant was trimmed or misselected", req.Tools)
			}
		})
	}
	tool := codexPayloadTool("payload")
	tool.ConstrainedSampling.Variants = map[string]string{"openai_lark": " \n", "openai_regex": "\t"}
	input := &goai.Context{Tools: []goai.Tool{tool}}
	if _, err := newCodexToolView(codexGrammarModel(), input); err == nil {
		t.Fatal("blank variants accepted")
	}
	err := streamViaWebSocket(context.Background(), codexGrammarModel(), input, nil, "fixture", func(goai.Event) {})
	if err == nil || !isCodexNonTransportError(err) || !strings.Contains(err.Error(), "variant") {
		t.Fatal("bad variant escaped preflight", err)
	}
	model := codexGrammarModel()
	no := false
	model.ResponsesCompat.SupportsOpenAIGrammarTools = &no
	if _, err = newCodexToolView(model, input); err != nil {
		t.Fatal("unsupported grammar should fall back", err)
	}
	req := buildCodexRequest(model, input, nil)
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" {
		t.Fatal("unsupported grammar fallback", req.Tools)
	}
}

func TestCodexHistoricalGrammarPreflightMain(t *testing.T) {
	good := codexPayloadTool("payload")
	schema := codexPayloadTool("payload")
	schema.Parameters = json.RawMessage(`{"type":"object","required":["payload"],"properties":{"payload":{"type":"number"}}}`)
	variant := codexPayloadTool("payload")
	variant.ConstrainedSampling.Variants = nil
	for _, tool := range []goai.Tool{schema, variant} {
		input := &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{tool}}, {Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "script"}}}}}
		model := codexGrammarModel()
		err := streamViaWebSocket(context.Background(), model, input, nil, "fixture", func(goai.Event) {})
		if err == nil || !isCodexNonTransportError(err) || !strings.Contains(err.Error(), "grammar") {
			t.Fatal("invalid removed definition escaped preflight", err)
		}
		failures := 0
		streamViaSSE(context.Background(), model, input, nil, "fixture", func(event goai.Event) {
			if e, ok := event.(*goai.ErrorEvent); ok {
				failures++
				if !strings.Contains(e.Err.Error(), "grammar") {
					t.Error(e.Err)
				}
			}
		}, nil)
		if failures != 1 {
			t.Fatal("SSE history escaped preflight", failures)
		}
		input.Messages = append(input.Messages, goai.Message{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{good}})
		if _, err = newCodexToolView(model, input); err != nil {
			t.Fatal("overwritten definition was validated", err)
		}
		input.Messages = input.Messages[:2]
		no := false
		model.ResponsesCompat.SupportsOpenAIGrammarTools = &no
		if _, err = newCodexToolView(model, input); err != nil {
			t.Fatal("unsupported grammar should fall back", err)
		}
	}
}
