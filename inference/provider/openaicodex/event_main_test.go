package openaicodex

import (
	"context"
	"errors"
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

func TestCodexEventEnvelopeFilterBothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		for _, terminal := range []string{"complete", "error"} {
			t.Run(transport+"/"+terminal, func(t *testing.T) {
				frames := []string{`[]`, `false`, `123`, `"text"`, `{}`, `{"type":null}`, `{"type":123}`, `{"type":true}`, `{"type":[]}`, `{"type":{}}`}
				wantType := "error"
				if terminal == "complete" {
					wantType = "response.completed"
					frames = append(frames, `{"type":"response.completed","response":{"status":"completed"}}`)
				} else {
					frames = append(frames, `{"type":"error","code":"bad_event","message":"bad"}`)
				}
				seen := []string{}
				events := runCodexObserverFrames(t, transport, context.Background(), &goai.StreamOptions{OnProviderStreamEvent: func(value any, _ *goai.Model) error {
					seen = append(seen, value.(map[string]interface{})["type"].(string))
					return nil
				}}, frames)
				if !reflect.DeepEqual(seen, []string{wantType}) {
					t.Fatal("untyped envelope reached observer", seen)
				}
				starts, done, failures := 0, 0, 0
				for _, event := range events {
					switch event.(type) {
					case *goai.StartEvent:
						starts++
					case *goai.DoneEvent:
						done++
					case *goai.ErrorEvent:
						failures++
					}
				}
				wantStarts, wantDone, wantErrors := 1, 1, 0
				if terminal == "error" {
					wantDone, wantErrors = 0, 1
					if transport == "websocket" {
						wantStarts = 0
					}
				}
				if starts != wantStarts || done != wantDone || failures != wantErrors {
					t.Fatal("ignored envelope started stream", starts, done, failures)
				}
			})
		}
	}
}

func TestCodexMalformedEventFailsBothTransportsMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		for _, started := range []bool{false, true} {
			name := "before"
			if started {
				name = "after"
			}
			t.Run(transport+"/"+name, func(t *testing.T) {
				frames := []string{}
				if started {
					frames = append(frames, `{"type":"response.created","response":{"id":"r"}}`)
				}
				frames = append(frames, `{"type":`)
				seen := 0
				events := runCodexObserverFrames(t, transport, context.Background(), &goai.StreamOptions{OnProviderStreamEvent: func(any, *goai.Model) error { seen++; return nil }}, frames)
				failures, done := 0, 0
				for _, event := range events {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						failures++
						var protocol *codexProtocolError
						if !errors.As(e.Err, &protocol) || protocol.payload != `{"type":` || e.Error == nil {
							t.Fatal("malformed event not a partial protocol failure", e)
						}
						if started && e.Error.ResponseID != "r" {
							t.Fatal("protocol error lost response identity", e)
						}
					case *goai.DoneEvent:
						done++
					}
				}
				wantSeen := 0
				if started {
					wantSeen = 1
				}
				if failures != 1 || done != 0 || seen != wantSeen {
					t.Fatal("malformed event ignored/observed", failures, done, seen)
				}
			})
		}
	}
}

func TestCodexObserverCanReplaceEventTypeMain(t *testing.T) {
	for _, transport := range []string{"sse", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			seen := 0
			events := runCodexObserverFrames(t, transport, context.Background(), &goai.StreamOptions{OnProviderStreamEvent: func(value any, _ *goai.Model) error {
				seen++
				event := value.(map[string]interface{})
				if event["type"] == "error" {
					event["type"] = 123
				}
				return nil
			}}, []string{`{"type":"error","message":"replaced"}`, `{"type":"response.completed","response":{"id":"final","status":"completed"}}`})
			starts, done := 0, 0
			for _, event := range events {
				switch e := event.(type) {
				case *goai.StartEvent:
					starts++
				case *goai.ErrorEvent:
					t.Fatal("mutated type became protocol failure", e)
				case *goai.DoneEvent:
					done++
					if e.Message.ResponseID != "final" {
						t.Fatal(e.Message)
					}
				}
			}
			if seen != 2 || starts != 1 || done != 1 {
				t.Fatal("post-observer type filter differs", seen, starts, done)
			}
		})
	}
}

func TestCodexNullEnvelopeTransportPolicyMain(t *testing.T) {
	for _, started := range []bool{false, true} {
		name := "before"
		if started {
			name = "after"
		}
		t.Run(name, func(t *testing.T) {
			var sse atomic.Int32
			finished := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					sse.Add(1)
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"fallback\",\"status\":\"completed\"}}\n\n"))
					return
				}
				defer close(finished)
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
				defer cancel()
				if _, _, err = conn.Read(ctx); err != nil {
					t.Error(err)
					return
				}
				if started {
					if err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"r"}}`)); err != nil {
						t.Error(err)
						return
					}
				}
				_ = conn.Write(ctx, websocket.MessageText, []byte(`null`))
			}))
			defer func() { server.Close(); <-finished }()
			model := codexGrammarModel()
			model.BaseURL = server.URL
			session := t.Name()
			defer ResetOpenAICodexWebSocketDebugStats(session)
			defer CloseOpenAICodexWebSocketSessions(session)
			seen := 0
			opts := &goai.StreamOptions{APIKey: payloadJWT, SessionID: session, Transport: goai.TransportAuto, OnProviderStreamEvent: func(any, *goai.Model) error { seen++; return nil }}
			done, failures := 0, 0
			for event := range streamCodex(context.Background(), model, &goai.Context{}, opts) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done++
					if started || e.Message.ResponseID != "fallback" {
						t.Fatal(e.Message)
					}
				case *goai.ErrorEvent:
					failures++
					var null *codexNullEventError
					if !started || !errors.As(e.Err, &null) || e.Error == nil || e.Error.ResponseID != "r" || len(e.Error.Diagnostics) != 1 {
						t.Fatal("null adapter exception lost partial", e)
					}
				}
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			wantDone, wantFailures, wantFallback := 1, 0, 1
			if started {
				wantDone, wantFailures, wantFallback = 0, 1, 0
			}
			if seen != 1 || done != wantDone || failures != wantFailures || int(sse.Load()) != wantFallback || stats == nil || stats.WebSocketFailures != 1 || stats.SSEFallbacks != wantFallback {
				t.Fatal("null envelope transport policy differs", seen, done, failures, sse.Load(), stats)
			}
		})
	}
}

func TestCodexSSEIgnoresDoneSentinelMain(t *testing.T) {
	seen := 0
	events := runCodexObserverFrames(t, "sse", context.Background(), &goai.StreamOptions{OnProviderStreamEvent: func(any, *goai.Model) error { seen++; return nil }}, []string{"   ", " [DONE] ", "\t", `{"type":"response.completed","response":{"id":"after_sentinel","status":"completed"}}`})
	done := 0
	for _, event := range events {
		switch e := event.(type) {
		case *goai.ErrorEvent:
			t.Fatal(e)
		case *goai.DoneEvent:
			done++
			if e.Message.ResponseID != "after_sentinel" {
				t.Fatal(e.Message)
			}
		}
	}
	if done != 1 || seen != 1 {
		t.Fatal("SSE sentinel ended stream or reached observer", done, seen)
	}
}
