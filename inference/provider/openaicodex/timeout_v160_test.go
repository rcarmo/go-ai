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

	coderws "github.com/coder/websocket"
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

// ai.codex.ws-connect-timeout (default retry policy). The other branches
// check an available retry budget and distinguish parent cancellation.
func TestSourceV160CodexConnectTimeout(t *testing.T) {
	for _, mode := range []string{"default", "retry-budget", "caller-cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			var dials, fetches atomic.Int32
			entered := make(chan struct{})
			upgradeCanceled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") == "websocket" {
					first := dials.Add(1) == 1
					if first {
						close(entered)
					}
					// Deliberately never send an upgrade response. Verify that
					// the client cancels this pending handshake at the peer.
					<-r.Context().Done()
					if first {
						close(upgradeCanceled)
					}
					return
				}
				fetches.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				for _, frame := range sourceCodexRecoveryFrames("resp_sse", "msg_sse", "Hello") {
					_, _ = fmt.Fprintf(w, "data: %s\n\n", frame)
				}
			}))
			defer server.Close()
			connectMs, requestMs := 50, 300_000
			opts := &goai.StreamOptions{APIKey: payloadJWT, SessionID: t.Name(), Transport: goai.TransportAuto, WebSocketConnectTimeoutMs: &connectMs, TimeoutMs: &requestMs}
			defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
			defer CloseOpenAICodexWebSocketSessions(opts.SessionID)
			if mode == "retry-budget" {
				cfg := goai.DefaultRetryConfig()
				cfg.MaxRetries, cfg.InitialDelay, cfg.MaxDelay, cfg.JitterFraction = 1, time.Millisecond, time.Millisecond, 0
				opts.RetryConfig = &cfg
			}
			if mode == "caller-cancel" {
				connectMs = 300_000
				go func() {
					select {
					case <-entered:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			model := &goai.Model{ID: "gpt-5.1-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL, Reasoning: true}
			input := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Say hello")}}
			var events []string
			var result *goai.Message
			var failure *goai.ErrorEvent
			for ev := range goai.Stream(ctx, model, input, opts) {
				switch e := ev.(type) {
				case *goai.StartEvent:
					events = append(events, "start")
				case *goai.DoneEvent:
					events = append(events, "done")
					result = e.Message
				case *goai.ErrorEvent:
					events = append(events, string(e.Reason))
					failure = e
				}
			}
			select {
			case <-upgradeCanceled:
			case <-time.After(time.Second):
				t.Error("pending upgrade did not cancel at the peer")
			}
			stats := GetOpenAICodexWebSocketDebugStats(opts.SessionID)
			if mode == "caller-cancel" {
				if !reflect.DeepEqual(events, []string{"aborted"}) || failure == nil || !errors.Is(failure.Err, context.Canceled) || dials.Load() != 1 || fetches.Load() != 0 || stats != nil {
					t.Errorf("caller-cancel: events %v, failure %+v, dials %d, SSE %d, stats %+v; want caller cancellation only", events, failure, dials.Load(), fetches.Load(), stats)
				}
				return
			}
			const wantError = "WebSocket connect timeout after 50ms"
			if dials.Load() != 1 || fetches.Load() != 1 {
				t.Errorf("dials and SSE requests = %d, %d; want 1, 1", dials.Load(), fetches.Load())
			}
			var firstText string
			if result != nil {
				for _, block := range result.Content {
					if block.Type == "text" {
						firstText = block.Text
						break
					}
				}
			}
			if !reflect.DeepEqual(events, []string{"start", "done"}) || firstText != "Hello" {
				t.Errorf("connect timeout: events %v, failure %+v; want successful Hello with one start", events, failure)
			}
			if stats == nil || stats.WebSocketFailures != 1 || stats.SSEFallbacks != 1 || !stats.WebSocketFallbackActive || stats.LastWebSocketError != wantError {
				t.Errorf("connect timeout stats = %+v; want one failure and fallback, active latch and exact %q", stats, wantError)
			}
			if result != nil {
				diagnostics := result.Diagnostics
				if len(diagnostics) != 1 {
					t.Errorf("connect timeout diagnostics count = %d; want one", len(diagnostics))
				} else if diag := diagnostics[0]; diag.Type != "provider_transport_failure" || diag.Error.Message != wantError || diag.Details["eventsEmitted"] != false || diag.Details["fallbackTransport"] != "sse" || diag.Details["phase"] != "before_message_stream_start" {
					t.Errorf("connect timeout diagnostic = %s, %q, %v; want pre-start timeout and SSE fallback", diag.Type, diag.Error.Message, diag.Details)
				}
			}
		})
	}
}

// Supplemental wire contract from the pinned parseWebSocket timeout catch.
// The peer withholds its close acknowledgment until the stream has settled:
// the close handshake must not delay either SSE fallback or terminal events.
func TestCodexIdleTimeoutWire(t *testing.T) {
	for _, mode := range []string{"cached-fallback", "one-shot-terminal", "abort-before-idle", "abort-after-idle"} {
		t.Run(mode, func(t *testing.T) {
			bounds, stop := context.WithTimeout(t.Context(), 2*time.Second)
			defer stop()
			ctx, cancel := context.WithCancel(bounds)
			defer cancel()
			streamSettled := make(chan struct{})
			wireClose := make(chan error, 1)
			var sends, fallbacks atomic.Int32
			started := mode == "one-shot-terminal" || mode == "abort-before-idle"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") != "websocket" {
					fallbacks.Add(1)
					if mode == "abort-after-idle" {
						// The idle attempt has already selected fallback. A later
						// caller abort must not cancel its pending close frame.
						cancel()
						return
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
				if _, _, err := conn.Read(bounds); err != nil {
					t.Errorf("read initial request: %v", err)
					return
				}
				sends.Add(1)
				if started {
					if err := conn.Write(bounds, websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"in_flight"}}`)); err != nil {
						t.Error(err)
						return
					}
				}
				select {
				case <-streamSettled:
					_, _, err := conn.Read(bounds)
					wireClose <- err
				case <-bounds.Done():
				}
			}))
			defer server.Close()
			idleMs := 50
			if mode == "abort-before-idle" {
				idleMs = 300_000
			}
			opts := &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportAuto, TimeoutMs: &idleMs}
			if !started {
				opts.SessionID = t.Name()
				defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
				defer CloseOpenAICodexWebSocketSessions(opts.SessionID)
			}
			model := &goai.Model{ID: "gpt-5.1-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL, Reasoning: true}
			var events []string
			var failure *goai.ErrorEvent
			for ev := range goai.Stream(ctx, model, &goai.Context{}, opts) {
				switch e := ev.(type) {
				case *goai.StartEvent:
					events = append(events, "start")
					if mode == "abort-before-idle" {
						cancel()
					}
				case *goai.DoneEvent:
					events = append(events, "done")
				case *goai.ErrorEvent:
					events = append(events, string(e.Reason))
					failure = e
				}
			}
			close(streamSettled)
			wantEvents, wantFallbacks := []string{"start", "done"}, int32(1)
			switch mode {
			case "one-shot-terminal":
				wantEvents, wantFallbacks = []string{"start", "error"}, 0
				if failure == nil || failure.Err == nil || failure.Err.Error() != "WebSocket idle timeout after 50ms" {
					t.Errorf("terminal idle error = %+v; want exact timeout", failure)
				}
			case "abort-before-idle":
				wantEvents, wantFallbacks = []string{"start", "aborted"}, 0
			case "abort-after-idle":
				wantEvents = []string{"aborted"}
			}
			if mode == "abort-before-idle" || mode == "abort-after-idle" {
				if failure == nil || !errors.Is(failure.Err, context.Canceled) {
					t.Errorf("caller cancellation = %+v; want context.Canceled", failure)
				}
			}
			if !reflect.DeepEqual(events, wantEvents) || sends.Load() != 1 || fallbacks.Load() != wantFallbacks {
				t.Errorf("events/sends/fallbacks = %v/%d/%d; want %v/1/%d", events, sends.Load(), fallbacks.Load(), wantEvents, wantFallbacks)
			}
			select {
			case err := <-wireClose:
				var close coderws.CloseError
				idleClose := errors.As(err, &close) && close.Code == websocket.StatusNormalClosure && close.Reason == "idle_timeout"
				if mode == "abort-before-idle" {
					if err == nil || idleClose {
						t.Errorf("peer close = %v; caller cancellation must not send idle_timeout", err)
					}
				} else if !idleClose {
					t.Errorf("peer close = %v; want close 1000 idle_timeout", err)
				}
			case <-bounds.Done():
				t.Error("peer close was not delivered independently of stream settlement")
			}
		})
	}
}
