package openaicodex

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func runCodexObserverFrames(t *testing.T, transport string, ctx context.Context, opts *goai.StreamOptions, frames []string) []goai.Event {
	t.Helper()
	started, finished := make(chan struct{}), make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) != 1 {
			t.Error("observer failure retried/fell back")
			http.Error(w, "unexpected fallback", 400)
			return
		}
		close(started)
		defer close(finished)
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"))
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		handlerCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, _, err = conn.Read(handlerCtx); err != nil {
			t.Error(err)
			return
		}
		for _, frame := range frames {
			if err = conn.Write(handlerCtx, websocket.MessageText, []byte(frame)); err != nil {
				return
			}
		}
	}))
	defer func() {
		server.Close()
		select {
		case <-started:
			<-finished
		default:
		}
	}()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	opts.APIKey = payloadJWT
	opts.SessionID = t.Name()
	if transport == "sse" {
		opts.Transport = goai.TransportSSE
	} else {
		opts.Transport = goai.TransportAuto
	}
	ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
	defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
	defer CloseOpenAICodexWebSocketSessions(opts.SessionID)
	events := []goai.Event{}
	var failed bool
	for event := range streamCodex(ctx, model, &goai.Context{}, opts) {
		events = append(events, event)
		if _, ok := event.(*goai.ErrorEvent); ok {
			failed = true
		}
	}
	if failed && transport != "sse" {
		codexWebSocketSessionsMu.Lock()
		entry := codexWebSocketSessions[opts.SessionID]
		codexWebSocketSessionsMu.Unlock()
		if entry != nil {
			t.Error("failed observer retained continuation session")
		}
	}
	stats := GetOpenAICodexWebSocketDebugStats(opts.SessionID)
	if stats != nil && (stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0) {
		t.Error("observer failure counted as transport", stats)
	}
	return events
}

func TestCodexRawObserverBothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		for _, mode := range []string{"success", "first_error", "later_error", "cancel", "cancel_error"} {
			t.Run(transport+"/"+mode, func(t *testing.T) {
				frames := []string{`{"type":"response.created","response":{"id":"r"}}`, `{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_i","call_id":"c","name":"script","input":"\ud800","label":"\udfff"}}`, `{"type":"response.custom_tool_call_input.delta","output_index":0,"delta":"\udc00"}`, `{"type":"response.custom_tool_call_input.done","output_index":0,"input":"\ud800\udc00\udfff"}`, `{"type":"response.output_item.done","output_index":0,"item":{"type":"custom_tool_call","input":"\ud800\udc00\udfff"}}`, `{"type":"response.done","response":{"id":"r","status":"unknown","usage":{}}}`}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				sentinel := errors.New("observer failed")
				seen := []string{}
				opts := &goai.StreamOptions{OnProviderStreamEvent: func(value any, model *goai.Model) error {
					event := value.(map[string]interface{})
					kind := event["type"].(string)
					seen = append(seen, kind)
					if model.ID != "codex-mini" {
						t.Error(model)
					}
					if mode == "first_error" {
						return sentinel
					}
					switch kind {
					case "response.output_item.added":
						item := event["item"].(map[string]interface{})
						input, ok := item["input"].(goai.UTF16String)
						if !ok || !reflect.DeepEqual(input.CodeUnits(), []uint16{0xd800}) || item["label"] != "�" {
							t.Error("raw argument scope lost", item)
						}
					case "response.custom_tool_call_input.delta":
						if mode == "later_error" {
							return sentinel
						}
						if strings.HasPrefix(mode, "cancel") {
							cancel()
							if mode == "cancel_error" {
								return sentinel
							}
						}
					case "response.done":
						response := event["response"].(map[string]interface{})
						if response["status"] != "unknown" {
							t.Error("observer ran after normalization", event)
						}
						response["id"] = "observed"
					}
					return nil
				}}
				events := runCodexObserverFrames(t, transport, ctx, opts, frames)
				done, failures, starts := 0, 0, 0
				var final *goai.Message
				var failure *goai.ErrorEvent
				delta := ""
				for _, event := range events {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
						final = e.Message
					case *goai.ErrorEvent:
						failures++
						failure = e
					case *goai.ToolCallStartEvent:
						starts++
					case *goai.ToolCallDeltaEvent:
						delta += e.Delta
					}
				}
				if mode == "success" {
					if done != 1 || failures != 0 || len(seen) != len(frames) || final.ResponseID != "observed" || final.RawStopReason != "" {
						t.Fatal("observer order/normalization/mutation", done, failures, seen, final)
					}
					block := final.Content[0]
					value, ok := block.Arguments["input"].(goai.UTF16String)
					if !ok || !block.HasStreamingArguments() || !reflect.DeepEqual(value.CodeUnits(), []uint16{0xd800, 0xdc00, 0xdfff}) {
						t.Fatal("observer damaged code units", block)
					}
					parsed, err := jsonparse.ParseJSONValue(delta, false)
					if err != nil || !reflect.DeepEqual(parsed, block.Arguments) {
						t.Fatal("observer changed JSON deltas", parsed, err)
					}
					return
				}
				if failures != 1 || done != 0 || failure.Error == nil {
					t.Fatal("observer failure lost partial", failures, done, failure)
				}
				if strings.HasPrefix(mode, "cancel") {
					if failure.Reason != goai.StopReasonAborted || !errors.Is(failure.Err, context.Canceled) {
						t.Fatal(failure)
					}
				} else if !errors.Is(failure.Err, sentinel) || failure.Reason != goai.StopReasonError {
					t.Fatal("observer error cause lost", failure)
				}
				if mode == "first_error" {
					if len(seen) != 1 || starts != 0 {
						t.Fatal("callback error still processed event", seen, starts)
					}
				} else {
					if len(seen) != 3 || starts != 1 || len(failure.Error.Content) != 1 || !failure.Error.Content[0].HasStreamingArguments() {
						t.Fatal("late observer error lost typed partial", seen, failure)
					}
				}
			})
		}
	}
}

func TestCodexObserverArgumentFieldsMain(t *testing.T) {
	for _, raw := range []string{
		`{"type":"response.function_call_arguments.delta","delta":"\udfff"}`,
		`{"type":"response.function_call_arguments.done","arguments":"\udfff"}`,
		`{"type":"response.custom_tool_call_input.delta","delta":"\udfff"}`,
		`{"type":"response.custom_tool_call_input.done","input":"\udfff"}`,
		`{"type":"response.output_item.added","item":{"type":"function_call","arguments":"\udfff"}}`,
		`{"type":"response.output_item.done","item":{"type":"function_call","arguments":"\udfff"}}`,
		`{"type":"response.output_item.added","item":{"type":"custom_tool_call","input":"\udfff"}}`,
		`{"type":"response.output_item.done","item":{"type":"custom_tool_call","input":"\udfff"}}`,
		`{"type":"response.done","response":{"status":"unknown","output":[{"type":"function_call","arguments":"\udfff"}]}}`,
		`{"type":"response.incomplete","response":{"output":[{"type":"custom_tool_call","input":"\udfff"}]}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			called := 0
			opts := &goai.StreamOptions{OnProviderStreamEvent: func(value any, _ *goai.Model) error {
				called++
				event := value.(map[string]interface{})
				if item, ok := event["item"].(map[string]interface{}); ok {
					event = item
				}
				if response, ok := event["response"].(map[string]interface{}); ok {
					event = response["output"].([]interface{})[0].(map[string]interface{})
				}
				for _, name := range []string{"delta", "input", "arguments"} {
					if value, ok := event[name]; ok {
						units, ok := value.(goai.UTF16String)
						if !ok || !reflect.DeepEqual(units.CodeUnits(), []uint16{0xdfff}) {
							t.Fatal("observer projection lost typed argument units", event)
						}
					}
				}
				event["observer_marker"] = "kept"
				return nil
			}}
			out, err := observeCodexEvent([]byte(raw), opts, nil)
			if err != nil || called != 1 {
				t.Fatal(err, called)
			}
			value, err := jsonparse.ParseJSONValue(string(out), true)
			if err != nil {
				t.Fatal(err)
			}
			event := value.(map[string]interface{})
			if item, ok := event["item"].(map[string]interface{}); ok {
				event = item
			}
			if response, ok := event["response"].(map[string]interface{}); ok {
				event = response["output"].([]interface{})[0].(map[string]interface{})
			}
			if event["observer_marker"] != "kept" {
				t.Fatal("observer mutation discarded", event)
			}
			for _, name := range []string{"delta", "input", "arguments"} {
				if value, ok := event[name]; ok {
					units, ok := value.(goai.UTF16String)
					if !ok || !reflect.DeepEqual(units.CodeUnits(), []uint16{0xdfff}) {
						t.Fatal("observer round-trip lost typed units", event)
					}
				}
			}
		})
	}
}
