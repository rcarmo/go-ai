package openaicodex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"nhooyr.io/websocket"
)

func TestStreamViaWebSocketCachedUsesDeltaAndDebugStats(t *testing.T) {
	CloseOpenAICodexWebSocketSessions("")
	ResetOpenAICodexWebSocketDebugStats("")
	defer CloseOpenAICodexWebSocketSessions("")

	received := make(chan map[string]interface{}, 2)
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

		for i, id := range []string{"resp_1", "resp_2"} {
			_, msg, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("read payload %d: %v", i+1, err)
			}
			var payload map[string]interface{}
			if err := json.Unmarshal(msg, &payload); err != nil {
				t.Fatalf("decode payload %d: %v", i+1, err)
			}
			received <- payload
			writeResponse(id, map[int]string{0: "ok", 1: "next-ok"}[i])
		}
	}))
	defer server.Close()

	model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	opts := &goai.StreamOptions{Transport: goai.TransportWebSocketCached, SessionID: "sess-1"}

	ch1 := make(chan goai.Event, 32)
	ctx1 := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	if err := streamViaWebSocket(context.Background(), model, ctx1, opts, jwt, ch1); err != nil {
		t.Fatalf("first streamViaWebSocket: %v", err)
	}
	close(ch1)
	var msg1 *goai.Message
	for ev := range ch1 {
		if d, ok := ev.(*goai.DoneEvent); ok {
			msg1 = d.Message
		}
	}
	if msg1 == nil || msg1.ResponseID != "resp_1" {
		t.Fatalf("first response = %#v", msg1)
	}

	ch2 := make(chan goai.Event, 32)
	ctx2 := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello"), *msg1, goai.UserMessage("next")}}
	if err := streamViaWebSocket(context.Background(), model, ctx2, opts, jwt, ch2); err != nil {
		t.Fatalf("second streamViaWebSocket: %v", err)
	}
	close(ch2)

	first := <-received
	second := <-received
	if _, ok := first["previous_response_id"]; ok {
		t.Fatalf("first request unexpectedly had previous_response_id: %#v", first)
	}
	if second["previous_response_id"] != "resp_1" {
		t.Fatalf("second previous_response_id = %#v payload=%#v", second["previous_response_id"], second)
	}
	input, _ := second["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("expected delta input with one item, got %#v", second["input"])
	}
	stats := GetOpenAICodexWebSocketDebugStats("sess-1")
	if stats == nil || stats.Requests != 2 || stats.ConnectionsCreated != 1 || stats.ConnectionsReused != 1 || stats.DeltaRequests != 1 || stats.FullContextRequests != 1 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
}

func TestStreamViaWebSocketProtocolFlow(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if err := streamViaWebSocket(context.Background(), model, convCtx, &goai.StreamOptions{}, jwt, ch); err != nil {
		t.Fatalf("streamViaWebSocket: %v", err)
	}
	close(ch)

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
