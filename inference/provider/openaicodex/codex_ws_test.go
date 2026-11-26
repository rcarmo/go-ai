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
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
)

func TestStreamViaWebSocketAutoUsesCachedDeltaAndDebugStats(t *testing.T) {
	testCodexWebSocketCachedEffort(t, nil)
}

// Source: pi v1.6.1 openai-codex-stream.test.ts:2464–2553, cached deltas across thinking levels.
func TestSourceV160CodexWebSocketCachedEffort(t *testing.T) {
	t.Run("source-low-high-high", func(t *testing.T) {
		testCodexWebSocketCachedEffort(t, []goai.ThinkingLevel{goai.ThinkingLow, goai.ThinkingHigh, goai.ThinkingHigh})
	})
	t.Run("supplemental-off", func(t *testing.T) {
		testCodexWebSocketCachedEffort(t, []goai.ThinkingLevel{goai.ThinkingLow, goai.ThinkingHigh, goai.ThinkingLevel(goai.ThinkingOff)})
	})
}

func testCodexWebSocketCachedEffort(t *testing.T, levels []goai.ThinkingLevel) {
	t.Helper()
	midEffort := len(levels) > 0
	session := t.Name()
	replies := []string{"resp_1", "resp_2"}
	if midEffort {
		replies = append(replies, "resp_3")
	}
	received := make(chan map[string]interface{}, len(replies))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			t.Error("unexpected SSE fallback")
			http.Error(w, "unexpected SSE fallback", http.StatusBadRequest)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		for i, id := range replies {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read payload %d: %v", i+1, err)
				return
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(msg, &payload); err != nil {
				t.Errorf("decode payload %d: %v", i+1, err)
				return
			}
			received <- payload
			itemID := fmt.Sprintf("msg_%d", i+1)
			for _, frame := range []string{
				fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, id),
				fmt.Sprintf(`{"type":"response.output_item.added","item":{"type":"message","id":%q,"role":"assistant","status":"in_progress","content":[]}}`, itemID),
				`{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`,
				`{"type":"response.output_text.delta","delta":"OK"}`,
				fmt.Sprintf(`{"type":"response.output_item.done","item":{"type":"message","id":%q,"role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK"}]}}`, itemID),
				fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","usage":{"input_tokens":5,"output_tokens":1,"total_tokens":6}}}`, id),
			} {
				if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
					t.Error(err)
					return
				}
			}
		}
	}))
	defer server.Close()
	defer ResetOpenAICodexWebSocketDebugStats(session)
	defer CloseOpenAICodexWebSocketSessions(session)

	model := &goai.Model{ID: "gpt-6.2-sol", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL, ContextWindow: 400_000, MaxTokens: 128_000}
	opts := &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportAuto, SessionID: session}

	if midEffort {
		model.Reasoning = true
		yes := true
		model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoEffort: &yes}
		opts.Transport = goai.TransportWebSocketCached
	}
	var messages []goai.Message
	for i, replyID := range replies {
		messages = append(messages, goai.Message{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: fmt.Sprintf("Turn %d", i+1)}}, Timestamp: int64(i)})
		if midEffort {
			opts.Reasoning = &levels[i]
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var done *goai.Message
		for ev := range goai.Stream(ctx, model, &goai.Context{Messages: messages}, opts) {
			switch e := ev.(type) {
			case *goai.DoneEvent:
				done = e.Message
			case *goai.ErrorEvent:
				t.Error(e.Err)
			}
		}
		cancel()
		if done == nil || done.ResponseID != replyID || done.StopReason != goai.StopReasonStop || goai.ContentText(done.Content) != "OK" {
			t.Fatalf("response = %+v; want successful %s with OK output", done, replyID)
		}
		messages = append(messages, *done)
		body := <-received
		var previous any
		if i > 0 {
			previous = replies[i-1]
		}
		if got, present := body["previous_response_id"]; present != (i > 0) || got != previous {
			t.Fatalf("previous_response_id = %v (present %t); want %v (present %t)", got, present, previous, i > 0)
		}
		input := body["input"].([]any)
		if midEffort {
			marker := string(levels[i])
			if marker == "off" {
				marker = "none"
			}
			want := []any{map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": fmt.Sprintf("Turn %d", i+1)}}}}
			if i > 0 && levels[i] != levels[i-1] {
				want = append(want, map[string]any{"type": "configuration_update", "reasoning": map[string]any{"effort": marker}})
			}
			if !reflect.DeepEqual(input, want) || !reflect.DeepEqual(body["reasoning"], map[string]any{"effort": "low", "summary": "auto"}) || done.ProviderThinkingLevel != marker {
				t.Fatalf("cached request = %v; thinking level=%q, want %q", body, done.ProviderThinkingLevel, marker)
			}
		} else if len(input) != 1 {
			t.Fatalf("expected delta input with one item, got %#v", input)
		}
	}
	stats := GetOpenAICodexWebSocketDebugStats(session)
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

// ai.codex.ws-normal-deltas: persist a grammar-tool call in the first turn,
// then send its output and the next user message as the entire second input.
func TestSourceV160CodexWebSocketNormalDeltas(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	session := t.Name()
	var sockets, sends, sse atomic.Int32
	bodies := make(chan map[string]json.RawMessage, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			sse.Add(1)
			http.Error(w, "unexpected SSE", http.StatusBadRequest)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		sockets.Add(1)
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			n := sends.Add(1)
			if n > 2 {
				t.Error("unexpected extra WebSocket input")
				return
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(data, &body); err != nil {
				t.Error(err)
				return
			}
			bodies <- body
			frames := []string{fmt.Sprintf(`{"type":"response.created","response":{"id":"resp_%d"}}`, n)}
			if n == 1 {
				frames = append(frames,
					`{"type":"response.output_item.added","item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"sample_tool","input":""}}`,
					`{"type":"response.custom_tool_call_input.delta","item_id":"ctc_1","delta":"abc"}`,
					`{"type":"response.custom_tool_call_input.done","item_id":"ctc_1","input":"abc"}`,
					`{"type":"response.output_item.done","item":{"type":"custom_tool_call","id":"ctc_1","call_id":"call_1","name":"sample_tool","input":"abc"}}`,
				)
			}
			frames = append(frames, fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8,"input_tokens_details":{"cached_tokens":0}}}}`, n))
			for _, frame := range frames {
				if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
					t.Error(err)
					return
				}
			}
		}
	}))
	defer server.Close()
	defer ResetOpenAICodexWebSocketDebugStats(session)
	defer CloseOpenAICodexWebSocketSessions(session)

	model := codexGrammarModel()
	model.ID, model.BaseURL, model.Reasoning = "gpt-5.1-codex", server.URL, true
	model.ContextWindow, model.MaxTokens = 400_000, 128_000
	tool := goai.Tool{
		Name: "sample_tool", Description: "Sample tool",
		Parameters:          json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`),
		ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /[a-z]+/"}},
	}
	input := goai.Context{
		SystemPrompt: "You are a helpful assistant.",
		Messages:     []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "Use the tool"}}, Timestamp: 1}},
		Tools:        []goai.Tool{tool},
	}
	opts := &goai.StreamOptions{APIKey: payloadJWT, SessionID: session, Transport: goai.TransportWebSocketCached}
	run := func() *goai.Message {
		t.Helper()
		var terminal *goai.Message
		for ev := range goai.Stream(ctx, model, &input, opts) {
			switch e := ev.(type) {
			case *goai.ErrorEvent:
				t.Fatalf("Codex stream: %v", e.Err)
			case *goai.DoneEvent:
				terminal = e.Message
			}
		}
		if terminal == nil {
			t.Fatal("missing terminal assistant message")
		}
		return terminal
	}
	first := run()
	if first.ResponseID != "resp_1" || len(first.Content) != 1 || first.Content[0].Type != "toolCall" || first.Content[0].ID != "call_1|ctc_1" || first.Content[0].Name != "sample_tool" || !reflect.DeepEqual(first.Content[0].Arguments, map[string]any{"payload": "abc"}) {
		t.Fatalf("first assistant message = %+v; want the exact grammar-tool call", first)
	}
	input.Messages = append(input.Messages, *first,
		goai.Message{Role: goai.RoleToolResult, ToolCallID: "call_1|ctc_1", ToolName: "sample_tool", Content: []goai.ContentBlock{{Type: "text", Text: "real result"}}, Timestamp: 2},
		goai.Message{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "Now finish"}}, Timestamp: 3},
	)
	run()
	if sends.Load() != 2 || sockets.Load() != 1 || sse.Load() != 0 {
		t.Fatalf("sends, sockets, SSE = %d, %d, %d; want 2, 1, 0", sends.Load(), sockets.Load(), sse.Load())
	}
	firstBody, secondBody := <-bodies, <-bodies
	if string(firstBody["store"]) != "false" || string(secondBody["store"]) != "false" {
		t.Errorf("store = %s then %s; want explicit false both times", firstBody["store"], secondBody["store"])
	}
	if _, present := firstBody["previous_response_id"]; present || string(secondBody["previous_response_id"]) != `"resp_1"` {
		t.Errorf("previous_response_id = %s then %s; want absent on first request and resp_1 on second", firstBody["previous_response_id"], secondBody["previous_response_id"])
	}
	assertInput := func(label string, actual json.RawMessage, expected string) {
		t.Helper()
		var got, want []any
		if err := json.Unmarshal(actual, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(expected), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s input = %s; want %s", label, actual, expected)
		}
	}
	assertInput("initial", firstBody["input"], `[{"role":"developer","content":"You are a helpful assistant."},{"role":"user","content":[{"type":"input_text","text":"Use the tool"}]}]`)
	assertInput("delta", secondBody["input"], `[{"type":"custom_tool_call_output","call_id":"call_1","output":"real result"},{"role":"user","content":[{"type":"input_text","text":"Now finish"}]}]`)
	stats := GetOpenAICodexWebSocketDebugStats(session)
	if stats == nil || stats.Requests != 2 || stats.ConnectionsCreated != 1 || stats.ConnectionsReused != 1 || stats.CachedContextRequests != 2 || stats.StoreTrueRequests != 0 || stats.FullContextRequests != 1 || stats.DeltaRequests != 1 || stats.LastDeltaInputItems == nil || *stats.LastDeltaInputItems != 2 || stats.LastPreviousResponseID != "resp_1" {
		t.Errorf("normal delta debug counters = %+v; want exact requests, caching, store, full/delta and continuation counters", stats)
	}
}
