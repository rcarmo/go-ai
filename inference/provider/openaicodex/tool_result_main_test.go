package openaicodex

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCodexToolResultWireTypeMain(t *testing.T) {
	for _, mode := range []string{"active_grammar", "function", "removed_grammar", "replaced_grammar", "unsupported", "unknown_name", "empty_name"} {
		t.Run(mode, func(t *testing.T) {
			model := codexGrammarModel()
			grammar := codexPayloadTool("program")
			plain := goai.Tool{Name: grammar.Name, Parameters: json.RawMessage(`{"type":"object"}`)}
			conv := &goai.Context{Tools: []goai.Tool{grammar}}
			result := goai.Message{Role: goai.RoleToolResult, ToolName: grammar.Name, ToolCallID: "call|item|suffix", Content: []goai.ContentBlock{{Type: "text", Text: "result"}}}
			want := "custom_tool_call_output"
			switch mode {
			case "function":
				conv.Tools = []goai.Tool{plain}
				want = "function_call_output"
			case "removed_grammar":
				conv.Tools = nil
				conv.Messages = []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{grammar}}, {Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: grammar.Name}}}}
			case "replaced_grammar":
				conv.Tools = nil
				conv.Messages = []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{grammar}}, {Role: goai.RoleSystem, ToolsAdded: []goai.Tool{plain}}}
				want = "function_call_output"
			case "unsupported":
				enabled := false
				model.ResponsesCompat.SupportsOpenAIGrammarTools = &enabled
				want = "function_call_output"
			case "unknown_name":
				result.ToolName = "other"
				want = "function_call_output"
			case "empty_name":
				result.ToolName = ""
				want = "function_call_output"
			}
			conv.Messages = append(conv.Messages, result)
			view, err := newCodexToolView(model, conv)
			if err != nil {
				t.Fatal(err)
			}
			input := buildCodexInputWithToolView(model, conv, view)
			last := input[len(input)-1].(map[string]interface{})
			if last["type"] != want || last["call_id"] != "call" || last["output"] != "result" {
				t.Fatal("tool result wire mismatch", last, want)
			}
		})
	}
}

func TestCodexToolResultContinuationMain(t *testing.T) {
	for _, custom := range []bool{false, true} {
		name := "function"
		if custom {
			name = "custom"
		}
		t.Run(name, func(t *testing.T) {
			captures := make(chan map[string]interface{}, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
				defer cancel()
				for n := 0; ; n++ {
					_, data, err := conn.Read(ctx)
					if err != nil {
						return
					}
					var body map[string]interface{}
					if err = json.Unmarshal(data, &body); err != nil {
						t.Error(err)
						return
					}
					captures <- body
					frames := []string{`{"type":"response.completed","response":{"id":"r2","status":"completed","output":[]}}`}
					if n == 0 {
						if custom {
							frames = []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"item","call_id":"call","name":"script"}}`, `{"type":"response.custom_tool_call_input.done","output_index":0,"input":"run"}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call"}}`}
						} else {
							frames = []string{`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","id":"item","call_id":"call","name":"script"}}`, `{"type":"response.function_call_arguments.done","output_index":0,"arguments":"{\"program\":\"run\"}"}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call"}}`}
						}
						frames = append(frames, `{"type":"response.completed","response":{"id":"r1","status":"completed","output":[]}}`)
					}
					for _, frame := range frames {
						if err = conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
							return
						}
					}
				}
			}))
			defer server.Close()
			model := codexGrammarModel()
			model.BaseURL = server.URL
			tool := codexPayloadTool("program")
			if !custom {
				tool.ConstrainedSampling = nil
			}
			conv := &goai.Context{Tools: []goai.Tool{tool}, Messages: []goai.Message{goai.UserMessage("hello")}}
			session := t.Name()
			defer ResetOpenAICodexWebSocketDebugStats(session)
			defer CloseOpenAICodexWebSocketSessions(session)
			run := func() *goai.Message {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				defer cancel()
				var final *goai.Message
				for event := range streamCodex(ctx, model, conv, &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportAuto, SessionID: session}) {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						t.Error(e.Err)
					case *goai.DoneEvent:
						final = e.Message
					}
				}
				if final == nil {
					t.Fatal("missing completion")
				}
				return final
			}
			first := run()
			if len(first.Content) != 1 || first.Content[0].Type != "toolCall" {
				t.Fatal("missing call", first)
			}
			select {
			case <-captures:
			case <-time.After(time.Second):
				t.Fatal("missing first request")
			}
			codexWebSocketSessionsMu.Lock()
			entry := cachedCodexSessionForTest(session)
			codexWebSocketSessionsMu.Unlock()
			if entry == nil || entry.continuation == nil || len(entry.continuation.lastResponseItems) != 1 {
				t.Fatal("synthetic result leaked into continuation")
			}
			item := entry.continuation.lastResponseItems[0].(map[string]interface{})
			callType := "function_call"
			outputType := "function_call_output"
			if custom {
				callType = "custom_tool_call"
				outputType = "custom_tool_call_output"
			}
			if item["type"] != callType {
				t.Fatal("continuation lost call type", item)
			}
			conv.Messages = append(conv.Messages, *first, goai.Message{Role: goai.RoleToolResult, ToolName: tool.Name, ToolCallID: first.Content[0].ID, Content: []goai.ContentBlock{{Type: "text", Text: "result"}}})
			_ = run()
			var body map[string]interface{}
			select {
			case body = <-captures:
			case <-time.After(time.Second):
				t.Fatal("missing tool-result continuation")
			}
			input, ok := body["input"].([]interface{})
			if !ok || len(input) != 1 || body["previous_response_id"] != "r1" {
				t.Fatal("tool result could not continue", body)
			}
			result := input[0].(map[string]interface{})
			if result["type"] != outputType || result["call_id"] != "call" || result["output"] != "result" {
				t.Fatal("wrong result delta", result)
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			if stats == nil || stats.ConnectionsCreated != 1 || stats.ConnectionsReused != 1 || stats.DeltaRequests != 1 || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 {
				t.Fatal("unexpected continuation stats", stats)
			}
		})
	}
}
