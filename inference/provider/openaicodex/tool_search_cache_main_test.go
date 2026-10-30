package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCodexToolSearchCachedAdditionsMain(t *testing.T) {
	captured := make(chan map[string]any, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		for n := 1; ; n++ {
			_, raw, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var body map[string]any
			if err = json.Unmarshal(raw, &body); err != nil {
				t.Error(err)
				return
			}
			captured <- body
			if err = conn.Write(ctx, websocket.MessageText, []byte(fmt.Sprintf(`{"type":"response.completed","response":{"id":"r%d","status":"completed","output":[]}}`, n))); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	enabled := true
	model.ResponsesCompat.SupportsMidConvoSystemMessages = &enabled
	model.ResponsesCompat.SupportsToolSearch = &enabled
	tool := func(name string) goai.Tool {
		return goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object"}`)}
	}
	conv := &goai.Context{Tools: []goai.Tool{tool("a")}, Messages: []goai.Message{goai.UserMessage("hello"), {Role: goai.RoleSystem, ToolsAdded: []goai.Tool{tool("b")}}}}
	session := t.Name()
	defer ResetOpenAICodexWebSocketDebugStats(session)
	defer CloseOpenAICodexWebSocketSessions(session)
	run := func() map[string]any {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		done := 0
		for event := range streamCodex(ctx, model, conv, &goai.StreamOptions{APIKey: payloadJWT, SessionID: session, Transport: goai.TransportAuto}) {
			switch e := event.(type) {
			case *goai.ErrorEvent:
				t.Error(e.Err)
			case *goai.DoneEvent:
				done++
			}
		}
		if done != 1 {
			t.Fatal("missing completion")
		}
		select {
		case body := <-captured:
			return body
		case <-ctx.Done():
			t.Fatal("missing request")
			return nil
		}
	}
	first := run()
	if len(first["input"].([]any)) != 3 {
		t.Fatal("missing initial tool load", first)
	}
	conv.Messages = append(conv.Messages, goai.Message{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{tool("c")}})
	second := run()
	input := second["input"].([]any)
	if second["previous_response_id"] != "r1" || len(input) != 2 {
		t.Fatal("late addition invalidated cached prefix", second)
	}
	call := input[0].(map[string]any)
	output := input[1].(map[string]any)
	id := responsestream.ToolLoadID("system:2", []string{"c"})
	if call["type"] != "tool_search_call" || call["call_id"] != id || output["type"] != "tool_search_output" || output["call_id"] != id {
		t.Fatal("new load was not a stable paired delta", input)
	}
	stats := GetOpenAICodexWebSocketDebugStats(session)
	if stats == nil || stats.ConnectionsCreated != 1 || stats.ConnectionsReused != 1 || stats.DeltaRequests != 1 || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 {
		t.Fatal("tool search lost socket/prefix", stats)
	}
}
