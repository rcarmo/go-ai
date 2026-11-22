package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
)

// ai.codex.ws-idle-before-first-event and ai.codex.ws-idle-after-start.
// The peer waits for cancellation or close instead of injecting any delay.
func TestSourceV160CodexIdleTimeout(t *testing.T) {
	for _, started := range []bool{false, true} {
		name := "before-first-event"
		if started {
			name = "after-start"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			var sends, fetches atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") != "websocket" {
					fetches.Add(1)
					if sends.Load() != 1 {
						t.Error("SSE fallback began before exactly one WebSocket send")
					}
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
				for {
					_, raw, err := conn.Read(ctx)
					if err != nil {
						return
					}
					var body struct{ Type string }
					if err := json.Unmarshal(raw, &body); err != nil || body.Type != "response.create" {
						t.Errorf("unexpected WebSocket request %s: %v", raw, err)
						return
					}
					if sends.Add(1) == 1 && started {
						if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_item.added","item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`)); err != nil {
							t.Error(err)
						}
					}
				}
			}))
			defer server.Close()
			idleMs := 50
			opts := &goai.StreamOptions{Transport: goai.TransportAuto, APIKey: payloadJWT, TimeoutMs: &idleMs}
			if !started {
				opts.SessionID = t.Name()
				defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
				defer CloseOpenAICodexWebSocketSessions(opts.SessionID)
			}
			model := &goai.Model{ID: "gpt-5.1-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL, Reasoning: true}
			input := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Say hello")}}
			var events []string
			var done *goai.Message
			var failure *goai.ErrorEvent
			for event := range goai.Stream(ctx, model, input, opts) {
				switch e := event.(type) {
				case *goai.StartEvent:
					events = append(events, "start")
				case *goai.DoneEvent:
					events = append(events, "done")
					done = e.Message
				case *goai.ErrorEvent:
					events = append(events, string(e.Reason))
					failure = e
				}
			}
			if sends.Load() != 1 {
				t.Errorf("WebSocket sends = %d; want 1", sends.Load())
			}
			const wantError = "WebSocket idle timeout after 50ms"
			if started {
				if !reflect.DeepEqual(events, []string{"start", "error"}) || failure == nil || failure.Error == nil || failure.Error.StopReason != goai.StopReasonError || failure.Error.ErrorMessage != wantError || failure.Err == nil || failure.Err.Error() != wantError || fetches.Load() != 0 {
					t.Errorf("after-start idle: events %v, failure %+v, SSE %d; want one start, exact idle error and no SSE", events, failure, fetches.Load())
				}
				return
			}
			var firstText string
			if done != nil {
				for _, block := range done.Content {
					if block.Type == "text" {
						firstText = block.Text
						break
					}
				}
			}
			if !reflect.DeepEqual(events, []string{"start", "done"}) || firstText != "Hello" || fetches.Load() != 1 {
				t.Errorf("before-start idle: events %v, failure %+v, SSE %d; want successful Hello with one SSE request", events, failure, fetches.Load())
			}
			stats := GetOpenAICodexWebSocketDebugStats(opts.SessionID)
			if stats == nil || stats.WebSocketFailures != 1 || stats.SSEFallbacks != 1 || !stats.WebSocketFallbackActive || stats.LastWebSocketError != wantError {
				t.Errorf("before-start idle counters = %+v; want one failure and one fallback with fallback active and the exact idle error", stats)
			}
		})
	}
}

// Supplemental: a frame starts a fresh timeout interval. The synthetic clock
// advances only when the channel waits, so no real-time tolerance is needed.
func TestCodexSocketIdleDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const idle = 50 * time.Millisecond
		lease := newCodexSocketLease()
		cause := errors.New("idle")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		start := time.Now()
		time.AfterFunc(idle/2, func() { lease.events <- codexSocketEvent{data: []byte("progress")} })
		data, err := readCodexSocketWithIdleTimeout(ctx, nil, lease, idle, cause)
		if string(data) != "progress" || err != nil || time.Since(start) != idle/2 {
			t.Fatalf("progress receipt = %q, %v at %v; want progress at %v", data, err, time.Since(start), idle/2)
		}
		if _, err := readCodexSocketWithIdleTimeout(ctx, nil, lease, idle, cause); err != cause || time.Since(start) != idle+idle/2 {
			t.Fatalf("read idle = %v at %v; want own timeout after a fresh interval", err, time.Since(start))
		}
		cancel()
		if _, err := readCodexSocketWithIdleTimeout(ctx, nil, lease, idle, cause); err != context.Canceled || time.Since(start) != idle+idle/2 {
			t.Fatalf("caller cancellation = %v at %v; want immediate cancellation, not idle", err, time.Since(start))
		}
	})
}
