package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
)

func TestCodexWebSocketLargeEventsAndReuse(t *testing.T) {
	for _, transport := range []goai.Transport{goai.TransportWebSocket, goai.TransportAuto, goai.TransportWebSocketCached} {
		t.Run(string(transport), func(t *testing.T) {
			session := "large-events-" + string(transport)
			defer CloseOpenAICodexWebSocketSessions(session)
			texts := []string{strings.Repeat("x", 40*1024), strings.Repeat("y", 1024*1024)}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Errorf("accept: %v", err)
					return
				}
				defer conn.CloseNow()
				// The second full-context request also contains the large prior answer.
				conn.SetReadLimit(4 * 1024 * 1024)
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()
				for {
					_, request, err := conn.Read(ctx)
					if err != nil {
						return
					}
					var body map[string]any
					if err := json.Unmarshal(request, &body); err != nil {
						t.Errorf("request JSON: %v", err)
						return
					}
					// The test sends the turn number as the last user input.
					text, id := texts[0], "large-0"
					if strings.Contains(string(request), "second-turn") {
						text, id = texts[1], "large-1"
					}
					for _, event := range []any{
						map[string]any{"type": "response.created", "response": map[string]any{"id": id}},
						map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "message", "id": id + "-item"}},
						map[string]any{"type": "response.output_text.delta", "delta": text},
						map[string]any{"type": "response.output_item.done", "item": map[string]any{"type": "message", "id": id + "-item"}},
						// Providers can repeat the complete answer in a large final envelope.
						map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": []any{map[string]any{"type": "message", "id": id + "-item", "content": []any{map[string]any{"type": "output_text", "text": text}}}}}},
					} {
						data, err := json.Marshal(event)
						if err != nil {
							t.Errorf("marshal: %v", err)
							return
						}
						if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
							return
						}
					}
				}
			}))
			defer server.Close()
			model := &goai.Model{ID: "codex-mini", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL}
			jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
			conversation := &goai.Context{Messages: []goai.Message{goai.UserMessage("first-turn")}}
			for i, text := range texts {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				events := make(chan goai.Event, 32)
				err := streamViaWebSocket(ctx, model, conversation, &goai.StreamOptions{Transport: transport, SessionID: session}, jwt, goai.NewAssistantEventSender(events))
				cancel()
				close(events)
				if err != nil {
					t.Fatal(err)
				}
				var answer *goai.Message
				var deltas strings.Builder
				for event := range events {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						t.Fatalf("turn %d: %v", i, e.Err)
					case *goai.TextDeltaEvent:
						deltas.WriteString(e.Delta)
					case *goai.DoneEvent:
						answer = e.Message
					}
				}
				if answer == nil || deltas.String() != text {
					t.Fatalf("turn %d: incomplete large answer", i)
				}
				if len(answer.Content) != 1 || answer.Content[0].Text != text {
					t.Fatalf("turn %d: final text changed", i)
				}
				conversation.Messages = append(conversation.Messages, *answer, goai.UserMessage("second-turn"))
			}
			if transport != goai.TransportWebSocket {
				stats := GetOpenAICodexWebSocketDebugStats(session)
				if stats.ConnectionsCreated != 1 || stats.ConnectionsReused != 1 {
					t.Fatalf("reuse stats: %+v", stats)
				}
			}
			ResetOpenAICodexWebSocketDebugStats(session)
		})
	}
}

func TestDialCodexWebSocketMessageLimit(t *testing.T) {
	const limit = 4 * 1024 * 1024
	for _, size := range []int{limit, limit + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Errorf("accept: %v", err)
					return
				}
				defer conn.CloseNow()
				_ = conn.Write(r.Context(), websocket.MessageText, []byte(strings.Repeat("x", size)))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn, err := dialCodexWebSocket(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil, goai.RetryConfig{}, &goai.Model{})
			if err != nil {
				t.Fatal(err)
			}
			defer conn.CloseNow()
			_, data, err := conn.Read(ctx)
			if size == limit {
				if err != nil || len(data) != size {
					t.Fatalf("boundary: bytes=%d error=%v", len(data), err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "read limited") {
				t.Fatalf("oversized message not bounded: %v", err)
			}
		})
	}
}
