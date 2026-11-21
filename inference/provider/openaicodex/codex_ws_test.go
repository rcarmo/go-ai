package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
)

func TestStreamViaWebSocketAutoUsesCachedDeltaAndDebugStats(t *testing.T) {
	testCodexWebSocketCachedEffort(t, false)
}

// Source: openai-codex-stream.test.ts, cached deltas across thinking levels.
func TestSourceV160CodexWebSocketCachedEffort(t *testing.T) {
	testCodexWebSocketCachedEffort(t, true)
}

func testCodexWebSocketCachedEffort(t *testing.T, midEffort bool) {
	t.Helper()
	CloseOpenAICodexWebSocketSessions("")
	ResetOpenAICodexWebSocketDebugStats("")
	defer CloseOpenAICodexWebSocketSessions("")

	replies := []string{"resp_1", "resp_2"}
	if midEffort {
		replies = append(replies, "resp_3")
	}
	received := make(chan map[string]interface{}, len(replies))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("accept websocket: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		writeResponse := func(id, text string) {
			write := func(v interface{}) {
				b, _ := json.Marshal(v)
				if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
					t.Fatalf("write ws frame: %v", err)
				}
			}
			write(map[string]interface{}{"type": "response.created", "response": map[string]interface{}{"id": id}})
			write(map[string]interface{}{"type": "response.output_item.added", "item": map[string]interface{}{"type": "message", "id": id + "_item"}})
			write(map[string]interface{}{"type": "response.output_text.delta", "delta": text})
			write(map[string]interface{}{"type": "response.output_item.done", "item": map[string]interface{}{"type": "message", "id": id + "_item"}})
			write(map[string]interface{}{"type": "response.completed", "response": map[string]interface{}{"id": id, "status": "completed", "usage": map[string]interface{}{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
		}

		for i, id := range replies {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("read payload %d: %v", i+1, err)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(msg, &payload); err != nil {
				t.Fatalf("decode payload %d: %v", i+1, err)
			}
			received <- payload
			writeResponse(id, fmt.Sprintf("reply %d", i+1))
		}
	}))
	defer server.Close()

	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	opts := &goai.StreamOptions{Transport: goai.TransportAuto, SessionID: "sess-1"}

	if midEffort {
		model.Reasoning = true
		yes := true
		model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoEffort: &yes}
		opts.Transport = goai.TransportWebSocketCached
	}
	opts.APIKey = jwt
	levels := []goai.ThinkingLevel{goai.ThinkingLow, goai.ThinkingHigh, goai.ThinkingLevel(goai.ThinkingOff)}
	var messages []goai.Message
	for i, replyID := range replies {
		messages = append(messages, goai.UserMessage(fmt.Sprintf("turn %d", i+1)))
		if midEffort {
			opts.Reasoning = &levels[i]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var done *goai.Message
		for ev := range streamCodexSimple(ctx, model, &goai.Context{Messages: messages}, opts) {
			switch e := ev.(type) {
			case *goai.DoneEvent:
				done = e.Message
			case *goai.ErrorEvent:
				t.Error(e.Err)
			}
		}
		cancel()
		if done == nil || done.ResponseID != replyID {
			t.Fatalf("response = %+v; want %s", done, replyID)
		}
		messages = append(messages, *done)
		body := <-received
		var previous any
		if i > 0 {
			previous = replies[i-1]
		}
		if body["previous_response_id"] != previous {
			t.Fatalf("previous_response_id=%v; want %v", body["previous_response_id"], previous)
		}
		input := body["input"].([]any)
		if midEffort {
			marker := string(levels[i])
			if marker == "off" {
				marker = "none"
			}
			want := []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": fmt.Sprintf("turn %d", i+1)}}}, map[string]any{"type": "configuration_update", "reasoning": map[string]any{"effort": marker}}}
			if !reflect.DeepEqual(input, want) || !reflect.DeepEqual(body["reasoning"], map[string]any{"effort": "medium", "summary": "auto"}) || done.ProviderThinkingLevel != marker {
				t.Fatalf("cached request = %v; thinking level=%q, want %q", body, done.ProviderThinkingLevel, marker)
			}
		} else if len(input) != 1 {
			t.Fatalf("expected delta input with one item, got %#v", input)
		}
	}
	stats := GetOpenAICodexWebSocketDebugStats("sess-1")
	if stats == nil || stats.Requests != len(replies) || stats.ConnectionsCreated != 1 || stats.ConnectionsReused != len(replies)-1 || stats.DeltaRequests != len(replies)-1 || stats.FullContextRequests != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestRemoveCodexWebSocketSessionClosesConnection(t *testing.T) {
	CloseOpenAICodexWebSocketSessions("")
	defer CloseOpenAICodexWebSocketSessions("")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		<-r.Context().Done()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	entry := &codexWebSocketSessionEntry{key: codexWebSocketSessionKey{sessionID: "sess-close"}, conn: conn}

	codexWebSocketSessionsMu.Lock()
	codexWebSocketSessions[entry.key] = entry
	codexWebSocketSessionsMu.Unlock()

	removeCodexWebSocketSession("sess-close", entry)

	codexWebSocketSessionsMu.Lock()
	_, ok := codexWebSocketSessions[entry.key]
	codexWebSocketSessionsMu.Unlock()
	if ok {
		t.Fatal("expected session entry to be removed")
	}
	if err := conn.Write(context.Background(), websocket.MessageText, []byte("ping")); err == nil {
		t.Fatal("expected write to fail after session removal closes websocket")
	}
}

func TestStreamCodexWebSocketSetupFailureFallsBackToSSEWithDiagnostic(t *testing.T) {
	CloseOpenAICodexWebSocketSessions("")
	ResetOpenAICodexWebSocketDebugStats("")
	defer CloseOpenAICodexWebSocketSessions("")

	var wsAttempts, sseAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			wsAttempts++
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatalf("accept websocket: %v", err)
			}
			_ = conn.Close(websocket.StatusPolicyViolation, "setup failed")
			return
		}
		sseAttempts++
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_sse\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"id\":\"item_1\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"item_1\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_sse\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer server.Close()

	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	opts := &goai.StreamOptions{Transport: goai.TransportAuto, SessionID: "sess-fallback"}

	var done *goai.Message
	for ev := range streamCodex(context.Background(), model, convCtx, &goai.StreamOptions{Transport: opts.Transport, SessionID: opts.SessionID, APIKey: jwt}) {
		if e, ok := ev.(*goai.DoneEvent); ok {
			done = e.Message
		}
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}
	if wsAttempts != 1 || sseAttempts != 1 {
		t.Fatalf("expected one websocket attempt and one SSE fallback, got ws=%d sse=%d", wsAttempts, sseAttempts)
	}
	if done == nil || done.ResponseID != "resp_sse" || len(done.Diagnostics) != 1 || done.Diagnostics[0].Type != "provider_transport_failure" {
		t.Fatalf("unexpected fallback done message: %#v", done)
	}
	stats := GetOpenAICodexWebSocketDebugStats("sess-fallback")
	if stats == nil || stats.WebSocketFailures != 1 || stats.SSEFallbacks != 1 || !stats.WebSocketFallbackActive {
		t.Fatalf("unexpected fallback stats: %#v", stats)
	}
}

func TestStreamViaWebSocketClosedBeforeCompletionErrorMatchesUpstream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("accept websocket: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()

	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	ch := make(chan goai.Event, 32)
	err := streamViaWebSocket(context.Background(), model, convCtx, &goai.StreamOptions{}, jwt, goai.NewAssistantEventSender(ch))
	if err == nil || err.Error() != "WebSocket stream closed before response.completed" {
		t.Fatalf("unexpected terminal WS error: %v", err)
	}
}

func TestStreamCodexRealWebSocketConnectionLimitRetrySucceedsWithoutSSE(t *testing.T) {
	CloseOpenAICodexWebSocketSessions("")
	ResetOpenAICodexWebSocketDebugStats("")
	defer CloseOpenAICodexWebSocketSessions("")

	var wsAttempts, sseAttempts int
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			wsAttempts++
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatalf("accept websocket: %v", err)
			}
			defer conn.Close(websocket.StatusNormalClosure, "done")
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			_, msg, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("read client payload: %v", err)
			}
			if wsAttempts == 1 {
				payload := map[string]interface{}{"type": "error", "error": map[string]interface{}{"code": "websocket_connection_limit_reached", "message": "too many websocket connections"}}
				b, _ := json.Marshal(payload)
				if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
					t.Fatalf("write connection-limit error: %v", err)
				}
				return
			}
			if err := json.Unmarshal(msg, &received); err != nil {
				t.Fatalf("decode client payload: %v", err)
			}
			write := func(v interface{}) {
				b, _ := json.Marshal(v)
				if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
					t.Fatalf("write ws frame: %v", err)
				}
			}
			write(map[string]interface{}{"type": "response.created", "response": map[string]interface{}{"id": "resp_ws_retry"}})
			write(map[string]interface{}{"type": "response.output_item.added", "item": map[string]interface{}{"type": "message", "id": "item_1"}})
			write(map[string]interface{}{"type": "response.output_text.delta", "delta": "retried-ok"})
			write(map[string]interface{}{"type": "response.output_item.done", "item": map[string]interface{}{"type": "message", "id": "item_1"}})
			write(map[string]interface{}{"type": "response.completed", "response": map[string]interface{}{"id": "resp_ws_retry", "status": "completed", "usage": map[string]interface{}{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
			return
		}
		sseAttempts++
		t.Fatalf("unexpected SSE fallback request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."

	var sawStart, sawText, sawDone bool
	for ev := range streamCodex(context.Background(), model, convCtx, &goai.StreamOptions{Transport: goai.TransportAuto, SessionID: "sess-conn-limit-success", APIKey: jwt}) {
		switch e := ev.(type) {
		case *goai.StartEvent:
			sawStart = true
		case *goai.TextDeltaEvent:
			sawText = sawText || e.Delta == "retried-ok"
		case *goai.DoneEvent:
			sawDone = true
			if e.Message == nil || e.Message.ResponseID != "resp_ws_retry" || len(e.Message.Diagnostics) != 0 {
				t.Fatalf("unexpected done message: %#v", e.Message)
			}
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}
	if wsAttempts != 2 || sseAttempts != 0 {
		t.Fatalf("expected one connection-limit WS rejection, one WS retry success, and no SSE fallback; got ws=%d sse=%d", wsAttempts, sseAttempts)
	}
	if received == nil || received["model"] != "codex-mini" {
		t.Fatalf("missing outbound response.create payload: %#v", received)
	}
	if !sawStart || !sawText || !sawDone {
		t.Fatalf("expected start/text/done after WS retry, got start=%v text=%v done=%v", sawStart, sawText, sawDone)
	}
	stats := GetOpenAICodexWebSocketDebugStats("sess-conn-limit-success")
	if stats == nil || stats.SSEFallbacks != 0 || stats.WebSocketFallbackActive {
		t.Fatalf("unexpected fallback stats after successful WS retry: %#v", stats)
	}
}

func TestStreamCodexRetriesWebSocketConnectionLimitOnceBeforeSSE(t *testing.T) {
	CloseOpenAICodexWebSocketSessions("")
	ResetOpenAICodexWebSocketDebugStats("")
	defer CloseOpenAICodexWebSocketSessions("")

	var wsAttempts, sseAttempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			wsAttempts++
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Fatalf("accept websocket: %v", err)
			}
			defer conn.Close(websocket.StatusNormalClosure, "done")
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			_, _, _ = conn.Read(ctx)
			payload := map[string]interface{}{"type": "error", "error": map[string]interface{}{"code": "websocket_connection_limit_reached", "message": "too many websocket connections"}}
			b, _ := json.Marshal(payload)
			if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
				t.Fatalf("write connection-limit error: %v", err)
			}
			return
		}
		sseAttempts++
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_sse\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"id\":\"item_1\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"message\",\"id\":\"item_1\"}}\n\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_sse\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n")
	}))
	defer server.Close()

	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."

	var done *goai.Message
	for ev := range streamCodex(context.Background(), model, convCtx, &goai.StreamOptions{Transport: goai.TransportAuto, SessionID: "sess-conn-limit", APIKey: jwt}) {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			done = e.Message
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}
	if wsAttempts != 2 || sseAttempts != 1 {
		t.Fatalf("expected two websocket attempts then one SSE fallback, got ws=%d sse=%d", wsAttempts, sseAttempts)
	}
	if done == nil || done.ResponseID != "resp_sse" || len(done.Diagnostics) != 1 || done.Diagnostics[0].Type != "provider_transport_failure" {
		t.Fatalf("unexpected fallback done message: %#v", done)
	}
}

func TestStreamViaWebSocketProtocolFlow(t *testing.T) {
	var received map[string]interface{}
	var gotUpgrade, gotConnection, gotKey, gotVersion, gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUpgrade = r.Header.Get("Upgrade")
		gotConnection = r.Header.Get("Connection")
		gotKey = r.Header.Get("Sec-WebSocket-Key")
		gotVersion = r.Header.Get("Sec-WebSocket-Version")
		gotHost = r.Host
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("accept websocket: %v", err)
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		_, msg, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read client payload: %v", err)
		}
		if err := json.Unmarshal(msg, &received); err != nil {
			t.Fatalf("decode client payload: %v", err)
		}

		write := func(v interface{}) {
			b, _ := json.Marshal(v)
			if err := conn.Write(ctx, websocket.MessageText, b); err != nil {
				t.Fatalf("write ws frame: %v", err)
			}
		}

		write(map[string]interface{}{"type": "response.created", "response": map[string]interface{}{"id": "resp_1"}})
		write(map[string]interface{}{"type": "response.output_item.added", "item": map[string]interface{}{"type": "message", "id": "item_1"}})
		write(map[string]interface{}{"type": "response.output_text.delta", "delta": "ok"})
		write(map[string]interface{}{"type": "response.output_item.done", "item": map[string]interface{}{"type": "message", "id": "item_1"}})
		write(map[string]interface{}{"type": "response.completed", "response": map[string]interface{}{"id": "resp_1", "status": "completed", "usage": map[string]interface{}{"input_tokens": 1, "output_tokens": 1, "total_tokens": 2}}})
	}))
	defer server.Close()

	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	ch := make(chan goai.Event, 32)
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	if err := streamViaWebSocket(context.Background(), model, convCtx, &goai.StreamOptions{}, jwt, goai.NewAssistantEventSender(ch)); err != nil {
		t.Fatalf("streamViaWebSocket: %v", err)
	}
	close(ch)

	if gotUpgrade != "websocket" || !strings.Contains(strings.ToLower(gotConnection), "upgrade") || gotKey == "" || gotVersion != "13" || gotHost == "" {
		t.Fatalf("missing RFC6455 handshake headers: upgrade=%q connection=%q key=%q version=%q host=%q", gotUpgrade, gotConnection, gotKey, gotVersion, gotHost)
	}
	if received["model"] != "codex-mini" {
		t.Fatalf("expected model in outbound payload, got %#v", received["model"])
	}

	var sawStart, sawText, sawDone bool
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.StartEvent:
			sawStart = true
		case *goai.TextDeltaEvent:
			sawText = sawText || e.Delta == "ok"
		case *goai.DoneEvent:
			sawDone = true
			if e.Message == nil || e.Message.StopReason != goai.StopReasonStop {
				t.Fatalf("unexpected done event: %#v", e)
			}
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}
	if !sawStart || !sawText || !sawDone {
		t.Fatalf("expected start/text/done events, got start=%v text=%v done=%v", sawStart, sawText, sawDone)
	}
}
