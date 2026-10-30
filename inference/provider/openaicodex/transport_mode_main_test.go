package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexTransportContinuationModesMain(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		modes                []goai.Transport
		previous             []string
		cached, full, delta  int
		missingID, failPlain bool
	}{
		{name: "omitted", modes: []goai.Transport{"", ""}, previous: []string{"", ""}, full: 2},
		{name: "plain", modes: []goai.Transport{goai.TransportWebSocket, goai.TransportWebSocket}, previous: []string{"", ""}, full: 2},
		{name: "auto", modes: []goai.Transport{goai.TransportAuto, goai.TransportAuto}, previous: []string{"", "r1"}, cached: 2, full: 1, delta: 1},
		{name: "cached", modes: []goai.Transport{goai.TransportWebSocketCached, goai.TransportWebSocketCached}, previous: []string{"", "r1"}, cached: 2, full: 1, delta: 1},
		{name: "auto_plain_auto", modes: []goai.Transport{goai.TransportAuto, goai.TransportWebSocket, goai.TransportAuto}, previous: []string{"", "", "r1"}, cached: 2, full: 2, delta: 1},
		{name: "auto_omitted_auto", modes: []goai.Transport{goai.TransportAuto, "", goai.TransportAuto}, previous: []string{"", "", "r1"}, cached: 2, full: 2, delta: 1},
		{name: "plain_auto_auto", modes: []goai.Transport{goai.TransportWebSocket, goai.TransportAuto, goai.TransportAuto}, previous: []string{"", "", "r2"}, cached: 2, full: 2, delta: 1},
		{name: "missing_response_id", modes: []goai.Transport{goai.TransportAuto, goai.TransportAuto, goai.TransportAuto}, previous: []string{"", "r1", "r1"}, cached: 3, full: 1, delta: 2, missingID: true},
		{name: "failed_plain_evicts", modes: []goai.Transport{goai.TransportAuto, goai.TransportWebSocket, goai.TransportAuto}, previous: []string{"", "", ""}, cached: 2, full: 3, failPlain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var connections, requests, sse atomic.Int32
			captures := make(chan accountCapture, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
					sse.Add(1)
					http.Error(w, "unexpected fallback", 400)
					return
				}
				connection := connections.Add(1)
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 7*time.Second)
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
					if body.Previous == "" && len(body.Input) != 1 {
						t.Error("full context was not sent", string(data))
					}
					captures <- accountCapture{connection: connection, previous: body.Previous}
					request := requests.Add(1)
					frame := fmt.Sprintf(`{"type":"response.completed","response":{"id":"r%d","status":"completed","output":[]}}`, request)
					if request == 2 && tc.missingID {
						frame = `{"type":"response.completed","response":{"status":"completed","output":[]}}`
					}
					if request == 2 && tc.failPlain {
						frame = `{"type":"error","code":"invalid_request","message":"plain failed"}`
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
			for i, mode := range tc.modes {
				ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
				done, failures := 0, 0
				for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: payloadJWT, SessionID: session, Transport: mode}) {
					switch e := event.(type) {
					case *goai.DoneEvent:
						done++
					case *goai.ErrorEvent:
						failures++
						if !tc.failPlain || i != 1 {
							t.Error(e.Err)
						}
					}
				}
				cancel()
				wantDone, wantErrors := 1, 0
				if tc.failPlain && i == 1 {
					wantDone, wantErrors = 0, 1
				}
				if done != wantDone || failures != wantErrors {
					t.Fatal("wrong completion", i, done, failures)
				}
				capture := nextAccountCapture(t, captures)
				wantConnection := int32(1)
				if tc.failPlain && i == 2 {
					wantConnection = 2
				}
				if capture.previous != tc.previous[i] || capture.connection != wantConnection {
					t.Fatal("transport context/socket selection differs", i, capture, tc.previous[i], wantConnection)
				}
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			wantConnections := 1
			if tc.failPlain {
				wantConnections = 2
			}
			if stats == nil || stats.CachedContextRequests != tc.cached || stats.FullContextRequests != tc.full || stats.DeltaRequests != tc.delta || stats.ConnectionsCreated != wantConnections || stats.ConnectionsReused != len(tc.modes)-wantConnections || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 || sse.Load() != 0 {
				t.Fatal("transport mode stats differ", stats, sse.Load())
			}
		})
	}
}
