package openaicodex

import (
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"strings"
	"testing"
)

func TestCodexProtocolErrorRetainsNativePartialMain(t *testing.T) {
	session := t.Name()
	ResetOpenAICodexWebSocketDebugStats(session)
	defer ResetOpenAICodexWebSocketDebugStats(session)
	defer CloseOpenAICodexWebSocketSessions(session)
	frames := []string{`{"type":"response.created","response":{"id":"r"}}`, `{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"ctc_i","call_id":"c","name":"script","input":"\udfff"}}`, `{"type":"response.custom_tool_call_input.done","output_index":0,"input":""}`}
	var start *goai.ToolCallStartEvent
	err := streamCodexArgumentFrames(t, "websocket", frames, func(event goai.Event) {
		if e, ok := goai.SnapshotEvent(event).(*goai.ToolCallStartEvent); ok {
			start = e
		}
	}, session)
	var protocol *codexProtocolError
	if err == nil || !errors.As(err, &protocol) || errors.Unwrap(err) == nil {
		t.Fatal("missing typed protocol failure", err)
	}
	event := codexStreamErrorEvent(err)
	partial := event.Error
	if event.Err != err || partial == nil || partial.StopReason != goai.StopReasonError || partial.ResponseID != "r" || len(partial.Content) != 1 || !strings.Contains(partial.ErrorMessage, "non-monotonically") {
		t.Fatal("protocol failure lost partial", event)
	}
	block := partial.Content[0]
	value, ok := block.Arguments["input"].(goai.UTF16String)
	if !ok || !block.HasStreamingArguments() || !reflect.DeepEqual(value.CodeUnits(), []uint16{0xdfff}) {
		t.Fatal("protocol failure lost argument ownership", block)
	}
	if start == nil || start.Partial.StopReason != goai.StopReasonPending {
		t.Fatal("terminal mutation reached detached progress", start)
	}
	encoded, encodeErr := json.Marshal(event)
	if encodeErr != nil || !strings.Contains(string(encoded), `\udfff`) {
		t.Fatal("error event cannot persist native argument", string(encoded), encodeErr)
	}
	stats := GetOpenAICodexWebSocketDebugStats(session)
	if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 || stats.WebSocketFallbackActive || len(partial.Diagnostics) != 0 {
		t.Fatal("protocol failure misclassified as transport", stats)
	}
	codexWebSocketSessionsMu.Lock()
	_, cached := codexWebSocketSessions[session]
	codexWebSocketSessionsMu.Unlock()
	if cached {
		t.Fatal("protocol failure retained cached session")
	}
}
func TestCodexPreflightProtocolErrorHasNoPartialMain(t *testing.T) {
	failure := &codexProtocolError{message: "preflight", cause: errors.New("bad schema")}
	event := codexStreamErrorEvent(failure)
	if event.Error != nil || event.Err != failure || event.Reason != goai.StopReasonError {
		t.Fatal(event)
	}
}
