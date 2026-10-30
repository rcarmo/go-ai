package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexRetentionNoneTransportScopeMain(t *testing.T) {
	for _, mode := range []string{"success", "seeded_fallback", "request_fallback", "started_failure", "long_session"} {
		t.Run(mode, func(t *testing.T) {
			var connections, requests, sse atomic.Int32
			captures := make(chan accountCapture, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					sse.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"sse\",\"status\":\"completed\",\"output\":[]}}\n\n")
					return
				}
				connection := connections.Add(1)
				if mode == "request_fallback" && connection == 2 {
					http.Error(w, "dial failed", 400)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
				defer cancel()
				for {
					_, data, err := conn.Read(ctx)
					if err != nil {
						return
					}
					var body struct {
						Previous string        `json:"previous_response_id"`
						Input    []interface{} `json:"input"`
					}
					if err = json.Unmarshal(data, &body); err != nil {
						t.Error(err)
						return
					}
					captures <- accountCapture{connection: connection, previous: body.Previous}
					n := requests.Add(1)
					if connection > 1 && (body.Previous != "" || len(body.Input) != 1) {
						t.Error("retention none reused context", string(data))
					}
					if mode == "started_failure" && connection == 2 {
						_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"partial"}}`))
						return
					}
					frame := fmt.Sprintf(`{"type":"response.completed","response":{"id":"r%d","status":"completed","output":[]}}`, n)
					if err = conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			session := t.Name()
			if mode == "long_session" {
				session = strings.Repeat("x", 90) + session
			}
			defer ResetOpenAICodexWebSocketDebugStats(session)
			defer CloseOpenAICodexWebSocketSessions(session)
			model := codexGrammarModel()
			model.BaseURL = server.URL
			zero := 0
			run := func(retention goai.CacheRetention) (int, int) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				done, failures := 0, 0
				for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: payloadJWT, SessionID: session, Transport: goai.TransportAuto, CacheRetention: retention, MaxRetries: &zero}) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
					case *goai.ErrorEvent:
						failures++
						if mode != "started_failure" || retention != goai.CacheRetentionNone {
							t.Error(e.Err)
						}
						if mode == "started_failure" && (e.Error == nil || len(e.Error.Diagnostics) != 1) {
							t.Error("started uncached failure lost diagnostic", e)
						}
					}
				}
				return done, failures
			}
			if done, failed := run(""); done != 1 || failed != 0 {
				t.Fatal("warm failed", done, failed)
			}
			warm := nextAccountCapture(t, captures)
			if warm.connection != 1 {
				t.Fatal(warm)
			}
			codexWebSocketSessionsMu.Lock()
			owner := cachedCodexSessionForTest(session)
			codexWebSocketSessionsMu.Unlock()
			if owner == nil {
				t.Fatal("raw session did not own warm socket")
			}
			if mode == "seeded_fallback" {
				recordCodexWebSocketFailure(session, errors.New("seeded session fallback"))
			}
			before := GetOpenAICodexWebSocketDebugStats(session)
			for i := 0; i < 2; i++ {
				done, failed := run(goai.CacheRetentionNone)
				wantDone, wantFailed := 1, 0
				if mode == "started_failure" && i == 0 {
					wantDone, wantFailed = 0, 1
				}
				if done != wantDone || failed != wantFailed {
					t.Fatal("uncached completion differs", i, done, failed)
				}
				if mode != "request_fallback" || i != 0 {
					capture := nextAccountCapture(t, captures)
					if capture.connection != int32(i+2) || capture.previous != "" {
						t.Fatal("retention none reused warm session", capture)
					}
				}
			}
			after := GetOpenAICodexWebSocketDebugStats(session)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("uncached request changed retained session counters", before, after)
			}
			codexWebSocketSessionsMu.Lock()
			retained := cachedCodexSessionForTest(session)
			codexWebSocketSessionsMu.Unlock()
			if retained != owner {
				t.Fatal("uncached request removed retained owner")
			}
			wantSSE := int32(0)
			if mode == "request_fallback" {
				wantSSE = 1
			}
			if connections.Load() != 3 || sse.Load() != wantSSE {
				t.Fatal("uncached requests inherited/created sticky fallback", connections.Load(), sse.Load())
			}
			if mode == "seeded_fallback" {
				if !isCodexWebSocketSSEFallbackActive(session) {
					t.Fatal("uncached request changed existing session fallback")
				}
			} else {
				if isCodexWebSocketSSEFallbackActive(session) {
					t.Fatal("uncached failure poisoned retained session")
				}
				if done, failed := run(""); done != 1 || failed != 0 {
					t.Fatal("retained session no longer usable")
				}
				capture := nextAccountCapture(t, captures)
				if capture.connection != 1 || capture.previous != "r1" {
					t.Fatal("uncached request changed retained continuation", capture)
				}
			}
			if mode == "long_session" && GetOpenAICodexWebSocketDebugStats(goai.ClampOpenAIPromptCacheKey(session)) != nil {
				t.Fatal("transport counters incorrectly clamped session")
			}
		})
	}
}
