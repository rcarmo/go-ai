package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type recoveryCapture struct {
	previous string
	inputs   int
}

func TestCodexMissingContinuationRetryBeforeStartMain(t *testing.T) {
	for _, mode := range []string{"once", "nested", "failed", "twice", "fresh", "combined", "cancel", "observer_previous", "observer_limit"} {
		t.Run(mode, func(t *testing.T) {
			var connections, requests, sse atomic.Int32
			captures := make(chan recoveryCapture, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					sse.Add(1)
					http.Error(w, "unexpected fallback", 400)
					return
				}
				connections.Add(1)
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
					_ = json.Unmarshal(data, &body)
					captures <- recoveryCapture{body.Previous, len(body.Input)}
					n := requests.Add(1)
					frame := `{"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}`
					fail := n == 2 && mode != "fresh" || mode == "twice" && n == 3 || mode == "fresh" && n == 1 || mode == "combined" && n == 3
					if fail {
						frame = `{"type":"error","code":"previous_response_not_found","message":"missing"}`
						if mode == "nested" {
							frame = `{"type":"error","error":{"code":"previous_response_not_found","message":"missing"}}`
						}
						if mode == "failed" {
							frame = `{"type":"response.failed","response":{"status":"failed","error":{"code":"previous_response_not_found","message":"missing"}}}`
						}
						if mode == "combined" && n == 2 {
							frame = `{"type":"error","code":"websocket_connection_limit_reached","message":"limit"}`
						}
					}
					if err = conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
						return
					}
				}
			}))
			defer server.Close()
			session := t.Name()
			defer ResetOpenAICodexWebSocketDebugStats(session)
			defer CloseOpenAICodexWebSocketSessions(session)
			model := codexGrammarModel()
			model.BaseURL = server.URL
			payloads := 0
			run := func(ctx context.Context, hook func(interface{}, *goai.Model) error) []goai.Event {
				events := []goai.Event{}
				for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportAuto, SessionID: session, OnProviderStreamEvent: hook, OnPayload: func(body interface{}, _ *goai.Model) (interface{}, error) { payloads++; return body, nil }}) {
					events = append(events, event)
				}
				return events
			}
			next := func() recoveryCapture {
				select {
				case value := <-captures:
					return value
				case <-time.After(2 * time.Second):
					t.Fatal("missing continuation request")
					return recoveryCapture{}
				}
			}
			warm := 0
			if mode != "fresh" {
				warm = 1
				events := run(context.Background(), nil)
				for _, event := range events {
					if e, ok := event.(*goai.ErrorEvent); ok {
						t.Fatal(e.Err)
					}
				}
				_ = next()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			observerCause := &codexAPIError{code: "previous_response_not_found", message: "observer cause"}
			if mode == "observer_limit" {
				observerCause.code = "websocket_connection_limit_reached"
			}
			events := run(ctx, func(value interface{}, _ *goai.Model) error {
				event := value.(map[string]interface{})
				if mode == "cancel" && event["type"] == "error" {
					cancel()
				}
				if strings.HasPrefix(mode, "observer_") {
					return observerCause
				}
				return nil
			})
			first := next()
			done, failures, starts := 0, 0, 0
			for _, event := range events {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done++
				case *goai.StartEvent:
					starts++
				case *goai.ErrorEvent:
					failures++
					if strings.HasPrefix(mode, "observer_") && !errors.Is(e.Err, observerCause) {
						t.Error("observer cause lost", e)
					}
					if mode == "cancel" && e.Reason != goai.StopReasonAborted {
						t.Error(e)
					}
				}
			}
			wantAttempts := 2
			if mode == "combined" {
				wantAttempts = 3
			}
			wantDone, wantFailures := 1, 0
			if mode == "twice" || mode == "cancel" || strings.HasPrefix(mode, "observer_") {
				wantDone, wantFailures = 0, 1
			}
			if mode == "cancel" || strings.HasPrefix(mode, "observer_") {
				wantAttempts = 1
			}
			if done != wantDone || failures != wantFailures || starts != wantDone || int(requests.Load()) != warm+wantAttempts || int(connections.Load()) != wantAttempts || payloads != warm+1 || sse.Load() != 0 {
				t.Fatal("missing continuation retry boundary differs", done, failures, starts, requests.Load(), connections.Load(), payloads, sse.Load())
			}
			if warm == 1 && first.previous != "r" {
				t.Fatal("fixture did not send continuation", first)
			}
			for i := 1; i < wantAttempts; i++ {
				retry := next()
				if retry.previous != "" || retry.inputs != 1 {
					t.Error("retry did not restore full context", retry)
				}
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 {
				t.Fatal("semantic recovery counted as network", stats)
			}
		})
	}
}
