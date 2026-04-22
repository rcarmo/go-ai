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
	if err := streamViaWebSocket(context.Background(), model, convCtx, &goai.StreamOptions{}, "test-key", ch); err != nil {
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
