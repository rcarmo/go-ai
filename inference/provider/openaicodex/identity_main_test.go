package openaicodex

import (
	"context"
	"encoding/json"
	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexCacheRetentionIdentityMain(t *testing.T) {
	for _, transport := range []goai.Transport{goai.TransportSSE, goai.TransportWebSocket, goai.TransportWebSocketCached, goai.TransportAuto} {
		for _, mode := range []string{"default", "none", "missing_session"} {
			t.Run(string(transport)+"/"+mode, func(t *testing.T) {
				type capture struct {
					header http.Header
					body   map[string]interface{}
				}
				var mu sync.Mutex
				var captures []capture
				var connections atomic.Int32
				record := func(header http.Header, data []byte) {
					var body map[string]interface{}
					if err := json.Unmarshal(data, &body); err != nil {
						t.Error(err)
					}
					mu.Lock()
					captures = append(captures, capture{header.Clone(), body})
					mu.Unlock()
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
						var reader io.Reader = r.Body
						if r.Header.Get("Content-Encoding") == "zstd" {
							decoder, err := zstd.NewReader(reader)
							if err != nil {
								t.Error(err)
								return
							}
							defer decoder.Close()
							reader = decoder
						}
						data, _ := io.ReadAll(reader)
						record(r.Header, data)
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
						return
					}
					connections.Add(1)
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.CloseNow()
					ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
					defer cancel()
					for {
						_, data, err := conn.Read(ctx)
						if err != nil {
							return
						}
						record(r.Header, data)
						if err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.completed","response":{"status":"completed"}}`)); err != nil {
							return
						}
					}
				}))
				defer server.Close()
				model := codexGrammarModel()
				model.BaseURL = server.URL
				opts := &goai.StreamOptions{APIKey: payloadJWT, Transport: transport, SessionID: t.Name()}
				if mode == "none" {
					opts.CacheRetention = goai.CacheRetentionNone
				}
				if mode == "missing_session" {
					opts.SessionID = ""
				}
				defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
				defer CloseOpenAICodexWebSocketSessions(opts.SessionID)
				for i := 0; i < 2; i++ {
					done := 0
					for event := range streamCodex(context.Background(), model, &goai.Context{}, opts) {
						switch e := event.(type) {
						case *goai.ErrorEvent:
							t.Error(e.Err)
						case *goai.DoneEvent:
							done++
						}
					}
					if done != 1 {
						t.Fatal("identity request failed", done)
					}
				}
				mu.Lock()
				records := append([]capture(nil), captures...)
				mu.Unlock()
				if len(records) != 2 {
					t.Fatal("wrong request count", len(records))
				}
				for _, record := range records {
					wantKey := goai.ClampOpenAIPromptCacheKey(opts.SessionID)
					if mode != "default" {
						wantKey = ""
					}
					if wantKey != "" {
						if record.body["prompt_cache_key"] != wantKey {
							t.Error("cache affinity key changed", record.body)
						}
					} else if _, exists := record.body["prompt_cache_key"]; exists {
						t.Error("cache key present without retention/session", record.body)
					}
					id := record.header.Get("session-id")
					if id != record.header.Get("x-client-request-id") {
						t.Error("request identifiers differ", record.header)
					}
					if transport == goai.TransportSSE || mode == "default" {
						if id != wantKey {
							t.Error("wrong affinity headers", id, wantKey)
						}
					} else if len(id) != 36 || id[14] != '7' {
						t.Error("missing UUIDv7 request identity", id)
					}
				}
				if transport != goai.TransportSSE {
					wantConnections := 1
					if mode != "default" {
						wantConnections = 2
						if records[0].header.Get("session-id") == records[1].header.Get("session-id") {
							t.Error("independent requests reused ephemeral identity")
						}
					}
					if int(connections.Load()) != wantConnections {
						t.Error("retention transport reuse differs", connections.Load(), wantConnections)
					}
				}
			})
		}
	}
}

func TestCodexEphemeralIdentityStableAcrossRetryMain(t *testing.T) {
	ids := make(chan string, 2)
	var connections atomic.Int32
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
		ids <- r.Header.Get("session-id")
		frame := `{"type":"response.completed","response":{"status":"completed"}}`
		if connections.Add(1) == 1 {
			frame = `{"type":"error","code":"websocket_connection_limit_reached","message":"retry"}`
		}
		_ = conn.Write(ctx, websocket.MessageText, []byte(frame))
	}))
	defer server.Close()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	opts := &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportAuto, SessionID: t.Name(), CacheRetention: goai.CacheRetentionNone}
	defer ResetOpenAICodexWebSocketDebugStats(opts.SessionID)
	done := 0
	for event := range streamCodex(context.Background(), model, &goai.Context{}, opts) {
		switch e := event.(type) {
		case *goai.ErrorEvent:
			t.Error(e.Err)
		case *goai.DoneEvent:
			done++
		}
	}
	if done != 1 || connections.Load() != 2 {
		t.Fatal("connection-limit retry did not complete", done, connections.Load())
	}
	first, second := <-ids, <-ids
	if first == "" || first != second {
		t.Fatal("retry changed request identity", first, second)
	}
}
