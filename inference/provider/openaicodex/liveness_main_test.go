package openaicodex

import (
	"context"
	"encoding/base64"
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

type liveCapture struct {
	connection              int32
	authorization, previous string
}

func rotatedAccountJWT(account, nonce string) string {
	data, _ := json.Marshal(map[string]interface{}{"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account}, "nonce": nonce})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(data) + "."
}
func TestCodexCachedLivenessAndCredentialRotationMain(t *testing.T) {
	for _, mode := range []string{"peer_close", "rotation", "idle_data"} {
		t.Run(mode, func(t *testing.T) {
			warmed, idleDone := make(chan struct{}), make(chan struct{})
			var connections atomic.Int32
			captures := make(chan liveCapture, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				number := connections.Add(1)
				ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
				defer cancel()
				if number == 1 && (mode == "peer_close" || mode == "idle_data") {
					go func() {
						defer close(idleDone)
						select {
						case <-warmed:
						case <-ctx.Done():
							return
						}
						if mode == "peer_close" {
							_ = conn.Close(websocket.StatusNormalClosure, "idle_peer_close")
							return
						}
						if err := conn.Write(ctx, websocket.MessageText, []byte(`{invalid_idle_data`)); err != nil {
							t.Error(err)
							return
						}
						if err := conn.Ping(ctx); err != nil {
							t.Error(err)
						}
					}()
				}
				for {
					_, data, err := conn.Read(ctx)
					if err != nil {
						return
					}
					var body struct {
						Previous string `json:"previous_response_id"`
					}
					_ = json.Unmarshal(data, &body)
					captures <- liveCapture{number, r.Header.Get("Authorization"), body.Previous}
					if err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}`)); err != nil {
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
			firstToken, secondToken := rotatedAccountJWT("A", "first"), rotatedAccountJWT("A", "second")
			seen := 0
			run := func(token string) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				done := 0
				for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: token, SessionID: session, Transport: goai.TransportAuto, OnProviderStreamEvent: func(value interface{}, _ *goai.Model) error {
					seen++
					if value.(map[string]interface{})["type"] != "response.completed" {
						t.Error("idle message crossed request lease", value)
					}
					return nil
				}}) {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						t.Error(e.Err)
					case *goai.DoneEvent:
						done++
					}
				}
				if done != 1 {
					t.Fatal("cached request failed", done)
				}
			}
			next := func() liveCapture {
				select {
				case result := <-captures:
					return result
				case <-time.After(2 * time.Second):
					t.Fatal("no live request")
					return liveCapture{}
				}
			}
			run(firstToken)
			first := next()
			key := codexWebSocketSessionKey{sessionID: session, accountID: "A", wsURL: resolveCodexWSURL(server.URL)}
			codexWebSocketSessionsMu.Lock()
			entry := codexWebSocketSessions[key]
			codexWebSocketSessionsMu.Unlock()
			if entry == nil {
				t.Fatal("warm socket not cached")
			}
			close(warmed)
			if mode != "rotation" {
				select {
				case <-idleDone:
				case <-time.After(2 * time.Second):
					t.Fatal("idle close/data not processed")
				}
			}
			if mode == "peer_close" {
				select {
				case <-entry.readDone:
				case <-time.After(2 * time.Second):
					t.Fatal("idle reader did not notice peer close")
				}
			}
			run(secondToken)
			second := next()
			if mode == "peer_close" {
				if second.connection == first.connection || second.previous != "" || second.authorization != "Bearer "+secondToken {
					t.Error("closed socket reused or stale credentials/context", first, second)
				}
			} else if second.connection != first.connection || second.previous != "r" || second.authorization != "Bearer "+firstToken {
				t.Error("same-account rotation did not retain account-scoped cached socket", first, second)
			}
			if !strings.HasPrefix(first.authorization, "Bearer ") {
				t.Error(first)
			}
			if seen != 2 {
				t.Error("idle data affected observer", seen)
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 {
				t.Error("idle close incorrectly activated fallback", stats)
			}
			CloseOpenAICodexWebSocketSessions(session)
			select {
			case <-entry.readDone:
			case <-time.After(2 * time.Second):
				t.Fatal("cache close did not join idle reader")
			}
		})
	}
}

func TestCodexCachedReaderBackpressureCancellationMain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
		for i := 0; i < 100; i++ {
			if err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"r"}}`)); err != nil {
				return
			}
		}
		_, _, _ = conn.Read(ctx)
	}))
	defer server.Close()
	session := t.Name()
	defer ResetOpenAICodexWebSocketDebugStats(session)
	defer CloseOpenAICodexWebSocketSessions(session)
	model := codexGrammarModel()
	model.BaseURL = server.URL
	var readerDone <-chan struct{}
	sentinel := errors.New("stop backed-up cached reader")
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	failures := 0
	for event := range streamCodex(ctx, model, &goai.Context{}, &goai.StreamOptions{APIKey: payloadJWT, SessionID: session, Transport: goai.TransportAuto, OnProviderStreamEvent: func(interface{}, *goai.Model) error {
		key := codexWebSocketSessionKey{sessionID: session, accountID: "acct_123", wsURL: resolveCodexWSURL(server.URL)}
		codexWebSocketSessionsMu.Lock()
		entry := codexWebSocketSessions[key]
		lease := entry.lease
		readerDone = entry.readDone
		codexWebSocketSessionsMu.Unlock()
		deadline := time.Now().Add(2 * time.Second)
		for len(lease.events) < cap(lease.events) {
			if time.Now().After(deadline) {
				t.Error("reader did not reach lease backpressure")
				break
			}
			time.Sleep(time.Millisecond)
		}
		return sentinel
	}}) {
		switch e := event.(type) {
		case *goai.ErrorEvent:
			failures++
			if !errors.Is(e.Err, sentinel) {
				t.Error(e.Err)
			}
		case *goai.DoneEvent:
			t.Error("cancelled lease completed")
		}
	}
	if failures != 1 || readerDone == nil {
		t.Fatal("missing cached reader failure", failures)
	}
	select {
	case <-readerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("backed-up cached reader did not stop")
	}
	stats := GetOpenAICodexWebSocketDebugStats(session)
	if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 {
		t.Fatal("observer stop counted as transport", stats)
	}
}
