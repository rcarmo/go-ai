package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
)

// packages/ai/test/openai-codex-stream.test.ts: recovers a missing cached
// websocket continuation via websocket / sse. The cached attempt starts on
// rate-limit metadata before it fails; the fresh attempt can fail before start.
func TestSourceV160CodexMissingCachedContinuation(t *testing.T) {
	for _, transport := range []string{"websocket", "sse"} {
		t.Run(transport, func(t *testing.T) {
			type receipt struct {
				connection int32
				body       map[string]json.RawMessage
			}
			var connections, requests, fetches atomic.Int32
			requestBodies := make(chan receipt, 4)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") != "websocket" {
					fetches.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					for _, frame := range sourceCodexRecoveryFrames("resp_sse", "msg_sse", "Hello") {
						_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
					}
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				id := connections.Add(1)
				for {
					_, data, err := conn.Read(r.Context())
					if err != nil {
						return
					}
					var body map[string]json.RawMessage
					if err := json.Unmarshal(data, &body); err != nil {
						t.Error(err)
						return
					}
					var frames []string
					switch requests.Add(1) {
					case 1:
						frames = sourceCodexRecoveryFrames("resp_1", "msg_1", "Hello")
					case 2:
						frames = []string{
							`{"type":"codex.rate_limits","plan_type":"plus","rate_limits":{"allowed":true,"limit_reached":false}}`,
							`{"type":"error","status":400,"error":{"code":"previous_response_not_found","message":"Previous response with id 'resp_1' not found.","param":"previous_response_id"}}`,
						}
					case 3:
						if transport == "websocket" {
							frames = sourceCodexRecoveryFrames("resp_2", "msg_2", "Recovered")
						}
					default:
						t.Error("unexpected extra WebSocket request")
						return
					}
					requestBodies <- receipt{connection: id, body: body}
					if frames == nil {
						// Transport dies *after* receiving the fresh request and
						// *before* reporting any provider event for this attempt.
						return
					}
					for _, frame := range frames {
						if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
							t.Error(err)
							return
						}
					}
				}
			}))
			defer server.Close()

			session := t.Name()
			defer ResetOpenAICodexWebSocketDebugStats(session)
			defer CloseOpenAICodexWebSocketSessions(session)
			model := &goai.Model{ID: "gpt-5.1-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL, Reasoning: true}
			opts := &goai.StreamOptions{Transport: goai.TransportWebSocketCached, SessionID: session, APIKey: payloadJWT}
			messages := []goai.Message{
				{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "You are a helpful assistant."}}},
				goai.UserMessage("Say hello"),
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var first *goai.Message
			for event := range goai.Stream(ctx, model, &goai.Context{Messages: messages}, opts) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					t.Fatalf("first turn: %v", e.Err)
				case *goai.DoneEvent:
					first = e.Message
				}
			}
			if first == nil {
				t.Fatal("missing first turn")
			}
			messages = append(messages, *first, goai.UserMessage("Now finish"))
			var events []string
			var second *goai.Message
			for event := range goai.Stream(ctx, model, &goai.Context{Messages: messages}, opts) {
				switch e := event.(type) {
				case *goai.StartEvent:
					events = append(events, "start")
				case *goai.ErrorEvent:
					events = append(events, "error")
					t.Errorf("unexpected terminal error: %v", e.Err)
				case *goai.DoneEvent:
					events = append(events, "done")
					second = e.Message
				}
			}
			wantText, wantFetches := "Recovered", int32(0)
			if transport == "sse" {
				wantText, wantFetches = "Hello", 1
			}
			var firstText *string
			if second != nil {
				for _, block := range second.Content {
					if block.Type == "text" {
						firstText = &block.Text
						break
					}
				}
			}
			if !reflect.DeepEqual(events, []string{"start", "done"}) || second == nil || second.StopReason != goai.StopReasonStop || firstText == nil || *firstText != wantText {
				t.Errorf("second turn: events %v, message %+v; want one start and successful %q", events, second, wantText)
			}
			if connections.Load() != 2 || requests.Load() != 3 || fetches.Load() != wantFetches {
				t.Fatalf("connections, requests, fetches = %d, %d, %d; want 2, 3, %d", connections.Load(), requests.Load(), fetches.Load(), wantFetches)
			}
			firstRequest, delta, replay := <-requestBodies, <-requestBodies, <-requestBodies
			if ids := []int32{firstRequest.connection, delta.connection, replay.connection}; !reflect.DeepEqual(ids, []int32{1, 1, 2}) {
				t.Errorf("request socket order = %v; want [1 1 2]", ids)
			}
			if string(delta.body["previous_response_id"]) != `"resp_1"` || replay.body["previous_response_id"] != nil {
				t.Errorf("continuations = %s then %s; want resp_1 then absent", delta.body["previous_response_id"], replay.body["previous_response_id"])
			}
			decodeInput := func(raw json.RawMessage) []any {
				t.Helper()
				var input []any
				if err := json.Unmarshal(raw, &input); err != nil {
					t.Fatal(err)
				}
				return input
			}
			gotDelta, gotReplay := decodeInput(delta.body["input"]), decodeInput(replay.body["input"])
			wantDelta := decodeInput(json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"Now finish"}]}]`))
			if !reflect.DeepEqual(gotDelta, wantDelta) || len(gotReplay) != 4 || !reflect.DeepEqual(gotReplay[3:], wantDelta) {
				t.Errorf("delta = %s, replay = %s; want exact delta and four-item full replay ending in the same user input", delta.body["input"], replay.body["input"])
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			if stats == nil || stats.Requests != 3 || stats.ConnectionsCreated != 2 || stats.ConnectionsReused != 1 || stats.FullContextRequests != 2 || stats.DeltaRequests != 1 || stats.WebSocketFailures != int(wantFetches) || stats.SSEFallbacks != int(wantFetches) {
				t.Errorf("debug stats = %+v; want requests:3 created:2 reused:1 full:2 delta:1 failed:%d fallback:%d", stats, wantFetches, wantFetches)
			}
		})
	}
}

func sourceCodexRecoveryFrames(responseID, messageID, text string) []string {
	return []string{
		fmt.Sprintf(`{"type":"response.created","response":{"id":%q}}`, responseID),
		fmt.Sprintf(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":%q,"role":"assistant","status":"in_progress","content":[]}}`, messageID),
		fmt.Sprintf(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":%q,"role":"assistant","status":"completed","content":[{"type":"output_text","text":%q}]}}`, messageID, text),
		fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`, responseID),
	}
}
