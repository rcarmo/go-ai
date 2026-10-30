package openaicodex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/websocket"
)

func runCodexArgumentFrames(t *testing.T, transport string, frames []string, send func(goai.Event)) {
	t.Helper()
	if err := streamCodexArgumentFrames(t, transport, frames, send); err != nil {
		t.Fatal(err)
	}
}
func streamCodexArgumentFrames(t *testing.T, transport string, frames []string, send func(goai.Event), sessions ...string) error {
	return streamCodexArgumentFramesWithTools(t, transport, frames, nil, send, sessions...)
}
func streamCodexArgumentFramesWithTools(t *testing.T, transport string, frames []string, tools []goai.Tool, send func(goai.Event), sessions ...string) error {
	t.Helper()
	yes := true
	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsOpenAIGrammarTools: &yes}}
	input := &goai.Context{Tools: tools, Messages: []goai.Message{goai.UserMessage("hello")}}
	if transport == "sse" {
		view, err := newCodexToolView(model, input)
		if err != nil {
			return err
		}
		processCodexSSE(strings.NewReader("data: "+strings.Join(frames, "\n\ndata: ")+"\n\n"), model, send, nil, view)
		return nil
	}
	handlerStarted, handlerDone := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		defer close(handlerDone)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, _, err = conn.Read(ctx); err != nil {
			t.Error(err)
			return
		}
		for _, frame := range frames {
			if err = conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
				// Negative fixtures intentionally close the peer on protocol rejection.
				// Client error classification and event assertions validate the outcome.
				return
			}
		}
	}))
	defer func() {
		server.Close()
		select {
		case <-handlerStarted:
			<-handlerDone
		default:
		}
	}()
	model.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	opts := &goai.StreamOptions{Transport: goai.TransportWebSocket}
	if len(sessions) > 0 {
		opts.SessionID = sessions[0]
		opts.Transport = goai.TransportWebSocketCached
	}
	return streamViaWebSocket(ctx, model, input, opts, jwt, send)
}

func TestCodexArgumentCodeUnitsBothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			frames := []string{
				`{"type":"response.output_item.added","item":{"type":"function_call","id":"i","call_id":"c","name":"leaf"}}`,
				`{"type":"response.function_call_arguments.delta","delta":"{\"k\":\"\ud800"}`,
				`{"type":"response.function_call_arguments.delta","delta":"\udc00\",\"low\":\""}`,
				`{"type":"response.function_call_arguments.done","arguments":"{\"k\":\"\ud800\udc00\",\"low\":\"\udfff\"}"}`,
				`{"type":"response.output_item.done","item":{"type":"function_call","arguments":"{\"k\":\"\ud800\udc00\",\"low\":\"\udfff\"}"}}`,
				`{"type":"response.completed","response":{"id":"r","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			}
			var deltas []*goai.ToolCallDeltaEvent
			var final *goai.Message
			var call *goai.ToolCall
			runCodexArgumentFrames(t, transport, frames, func(event goai.Event) {
				switch e := goai.SnapshotEvent(event).(type) {
				case *goai.ToolCallDeltaEvent:
					deltas = append(deltas, e)
				case *goai.ToolCallEndEvent:
					call = &e.ToolCall
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ErrorEvent:
					t.Error(e.Err)
				}
			})
			if len(deltas) != 3 || final == nil || call == nil {
				t.Fatal("missing deltas/final", len(deltas), final)
			}
			units, err := jsonparse.StringCodeUnits(deltas[0].Delta)
			if err != nil || units[len(units)-1] != 0xd800 {
				t.Fatal("first fragment unit lost", units, err)
			}
			if final.Content[0].Arguments["k"] != "𐀀" {
				t.Fatal("split pair lost", final.Content[0].Arguments)
			}
			low, ok := call.Arguments["low"].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(low.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("isolated final unit lost", call.Arguments)
			}
			if !call.HasStreamingArguments() || !final.Content[0].HasStreamingArguments() {
				t.Fatal("final argument ownership lost")
			}
			if _, exists := deltas[0].Partial.Content[0].Arguments["low"]; exists {
				t.Fatal("earlier snapshot mutated")
			}
		})
	}
}

func TestCodexAuthoritativeArgumentReplacementBothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		for _, mode := range []string{"arguments_done", "item_done", "empty", "invalid"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				scalar, err := jsonparse.ParseJSONValue(`"\udfff"`, true)
				if err != nil {
					t.Fatal(err)
				}
				low := scalar.(goai.UTF16String).WTF8()
				encode := func(value any) string {
					raw, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					return string(raw)
				}
				frames := []string{`{"type":"response.output_item.added","item":{"type":"function_call","id":"i","call_id":"c","name":"leaf"}}`, `{"type":"response.function_call_arguments.delta","delta":"{\"stale\":1}"}`}
				replacement := "{\"low\":\"" + low + "\"}"
				if mode == "empty" {
					replacement = ""
				}
				if mode == "invalid" {
					replacement = "{\"low\":\"" + low + "\""
				}
				frames = append(frames, encode(map[string]any{"type": "response.function_call_arguments.done", "arguments": jsonparse.ArgumentText(replacement)}))
				item := map[string]any{"type": "function_call"}
				if mode == "item_done" {
					item["arguments"] = jsonparse.ArgumentText("{\"final\":\"" + low + "\"}")
				}
				frames = append(frames, encode(map[string]any{"type": "response.output_item.done", "item": item}), `{"type":"response.completed","response":{"id":"r","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)
				deltas := 0
				var call *goai.ToolCall
				runCodexArgumentFrames(t, transport, frames, func(event goai.Event) {
					switch e := event.(type) {
					case *goai.ToolCallDeltaEvent:
						deltas++
					case *goai.ToolCallEndEvent:
						call = &e.ToolCall
					case *goai.ErrorEvent:
						t.Error(e.Err)
					}
				})
				if call == nil || deltas != 1 || !call.HasStreamingArguments() {
					t.Fatal("replacement emitted false continuation/lost ownership", deltas, call)
				}
				if mode == "invalid" {
					if !strings.Contains(call.ArgumentsError, low) || len(call.Arguments) != 0 {
						t.Fatal("invalid-call diagnostic lost units", call)
					}
					return
				}
				if call.ArgumentsError != "" {
					t.Fatal(call.ArgumentsError)
				}
				if mode == "empty" {
					if len(call.Arguments) != 0 {
						t.Fatal("empty replacement retained stale values", call.Arguments)
					}
					return
				}
				key := "low"
				if mode == "item_done" {
					key = "final"
				}
				value, ok := call.Arguments[key].(goai.UTF16String)
				if len(call.Arguments) != 1 || !ok || !reflect.DeepEqual(value.CodeUnits(), []uint16{0xdfff}) {
					t.Fatal("authoritative replacement lost units", call.Arguments)
				}
			})
		}
	}
}

func TestCodexInvalidArgumentEventIsNotTransportFailureMain(t *testing.T) {
	frames := []string{`{"type":"response.output_item.added","item":{"type":"function_call","id":"i","call_id":"c","name":"leaf"}}`, `{"type":"response.function_call_arguments.done","arguments":[]}`}
	session := t.Name()
	defer CloseOpenAICodexWebSocketSessions(session)
	err := streamCodexArgumentFrames(t, "websocket", frames, func(goai.Event) {}, session)
	if err == nil || !isCodexNonTransportError(err) {
		t.Fatal("argument protocol error could trigger transport fallback/replay", err)
	}
	codexWebSocketSessionsMu.Lock()
	_, cached := codexWebSocketSessions[session]
	codexWebSocketSessionsMu.Unlock()
	if cached {
		t.Fatal("invalid argument event retained cached connection")
	}
}
