package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
)

type codexScopeDial struct {
	account string
	auth    string
}

type codexScopeSend struct {
	connection int32
	body       map[string]json.RawMessage
}

type codexScopeClose struct {
	connection int32
	err        error
}

// Assert at the transport boundary: a missing cache entry alone does not
// establish that the peer received a close, or even that the socket closed.
func TestSourceV160CodexSocketScope(t *testing.T) {
	for _, mode := range []string{"account", "one-shot", "scoped-close-reset", "active-close"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var connections, requests, fetches atomic.Int32
			dials := make(chan codexScopeDial, 16)
			sends := make(chan codexScopeSend, 16)
			closed := make(chan codexScopeClose, 16)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Upgrade") != "websocket" {
					fetches.Add(1)
					http.Error(w, "unexpected SSE fallback", http.StatusBadRequest)
					return
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				id := connections.Add(1)
				dials <- codexScopeDial{account: r.Header.Get("ChatGPT-Account-Id"), auth: r.Header.Get("Authorization")}
				for {
					_, data, err := conn.Read(ctx)
					if err != nil {
						closed <- codexScopeClose{connection: id, err: err}
						return
					}
					var body map[string]json.RawMessage
					if err := json.Unmarshal(data, &body); err != nil {
						t.Error(err)
						return
					}
					sends <- codexScopeSend{connection: id, body: body}
					n := requests.Add(1)
					frame := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}}`, n)
					if mode == "active-close" && n == 1 {
						frame = `{"type":"response.created","response":{"id":"in_flight"}}`
					}
					if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
						t.Error(err)
						return
					}
				}
			}))
			defer server.Close()
			target, other := t.Name(), t.Name()+"/other"
			defer ResetOpenAICodexWebSocketDebugStats(target)
			defer ResetOpenAICodexWebSocketDebugStats(other)
			defer CloseOpenAICodexWebSocketSessions(target)
			defer CloseOpenAICodexWebSocketSessions(other)
			model := &goai.Model{ID: "gpt-5.1-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, BaseURL: server.URL, Reasoning: true}
			run := func(session, account string, oneShot bool) codexScopeSend {
				t.Helper()
				input := &goai.Context{}
				opts := &goai.StreamOptions{APIKey: accountJWT(account), SessionID: session, Transport: goai.TransportWebSocketCached}
				if oneShot {
					input.SystemPrompt = "You are a helpful assistant."
					input.Messages = []goai.Message{goai.UserMessage("Say hello")}
					opts.Transport = goai.TransportAuto
					opts.CacheRetention = goai.CacheRetentionNone
				}
				done := 0
				for e := range goai.Stream(ctx, model, input, opts) {
					switch ev := e.(type) {
					case *goai.ErrorEvent:
						t.Fatalf("Codex request: %v", ev.Err)
					case *goai.DoneEvent:
						done++
					}
				}
				if done != 1 {
					t.Fatalf("done events = %d; want 1", done)
				}
				select {
				case receipt := <-sends:
					return receipt
				case <-ctx.Done():
					t.Fatal("missing sent body")
					return codexScopeSend{}
				}
			}
			readCloses := func(count int) map[int32]error {
				t.Helper()
				out := map[int32]error{}
				for range count {
					select {
					case close := <-closed:
						out[close.connection] = close.err
					case <-ctx.Done():
						t.Fatal("missing peer close")
					}
				}
				return out
			}
			assertDebugClose := func(id int32, err error) {
				t.Helper()
				var close coderws.CloseError
				if !errors.As(err, &close) || close.Code != websocket.StatusNormalClosure || close.Reason != "debug_close" {
					t.Errorf("session close peer %d = %v; want close 1000 debug_close", id, err)
				}
			}
			switch mode {
			case "account": // ai.codex.ws-account-scope
				first := run(target, "account-a", false)
				second := run(target, "account-b", false)
				third := run(target, "account-a", false)
				if connections.Load() != 2 {
					t.Fatalf("authenticated handshakes = %d; want two", connections.Load())
				}
				got := []codexScopeDial{<-dials, <-dials}
				want := []codexScopeDial{{"account-a", "Bearer " + accountJWT("account-a")}, {"account-b", "Bearer " + accountJWT("account-b")}}
				if !reflect.DeepEqual(got, want) {
					t.Error("handshake account IDs or exact bearer values differ")
				}
				if ids := []int32{first.connection, second.connection, third.connection}; !reflect.DeepEqual(ids, []int32{1, 2, 1}) {
					t.Errorf("send socket order = %v; want [1 2 1]", ids)
				}
				if stats := GetOpenAICodexWebSocketDebugStats(target); stats == nil || stats.ConnectionsCreated != 2 || stats.ConnectionsReused != 1 {
					t.Errorf("account scope stats = %+v; want two created and one reused", stats)
				}
			case "one-shot": // ai.codex.ws-no-cache
				first := run(target, "account-a", true)
				second := run(target, "account-a", true)
				if connections.Load() != 2 || requests.Load() != 2 || first.connection != 1 || second.connection != 2 {
					t.Fatalf("one-shot handshakes, requests and sockets = %d, %d, %d then %d; want two fresh sockets each with one send", connections.Load(), requests.Load(), first.connection, second.connection)
				}
				if first.body["prompt_cache_key"] != nil || second.body["prompt_cache_key"] != nil {
					t.Errorf("one-shot affinity present: %s, %s", first.body["prompt_cache_key"], second.body["prompt_cache_key"])
				}
				peers := readCloses(2)
				for _, id := range []int32{1, 2} {
					if err := peers[id]; websocket.CloseStatus(err) < 0 && !errors.Is(err, io.EOF) {
						t.Errorf("one-shot socket %d peer close = %v", id, err)
					}
				}
				if stats := GetOpenAICodexWebSocketDebugStats(target); stats != nil {
					t.Errorf("one-shot stats = %+v; want no record", stats)
				}
			case "scoped-close-reset": // Supplemental: pinned close/reset source functions.
				a, b := run(target, "account-a", false), run(target, "account-b", false)
				survivor := run(other, "account-a", false)
				before := GetOpenAICodexWebSocketDebugStats(target)
				CloseOpenAICodexWebSocketSessions(target)
				peers := readCloses(2)
				assertDebugClose(a.connection, peers[a.connection])
				assertDebugClose(b.connection, peers[b.connection])
				if after := GetOpenAICodexWebSocketDebugStats(target); before == nil || !reflect.DeepEqual(before, after) {
					t.Errorf("close changed debug stats: before %+v after %+v", before, after)
				}
				ResetOpenAICodexWebSocketDebugStats(other)
				if GetOpenAICodexWebSocketDebugStats(other) != nil || !reflect.DeepEqual(GetOpenAICodexWebSocketDebugStats(target), before) {
					t.Error("reset did not clear exactly the requested session stats")
				}
				if reused := run(other, "account-a", false); reused.connection != survivor.connection {
					t.Errorf("other socket = %d; want reuse of %d after targeted close/reset", reused.connection, survivor.connection)
				}
				if fresh := run(target, "account-a", false); fresh.connection == a.connection || fresh.body["previous_response_id"] != nil {
					t.Errorf("closed session reused old socket or continuation: %+v", fresh)
				}
			case "active-close": // Supplemental: public cleanup while a cached reader owns a lease.
				opts := &goai.StreamOptions{APIKey: accountJWT("account-a"), SessionID: target, Transport: goai.TransportWebSocketCached}
				var events []string
				for event := range goai.Stream(ctx, model, &goai.Context{}, opts) {
					switch event := event.(type) {
					case *goai.StartEvent:
						events = append(events, "start")
						if err := goai.CleanupSessionResources(target); err != nil {
							t.Errorf("public resource cleanup: %v", err)
						}
					case *goai.ErrorEvent:
						events = append(events, string(event.Reason))
					case *goai.DoneEvent:
						events = append(events, "done")
					}
				}
				if !reflect.DeepEqual(events, []string{"start", "error"}) {
					t.Errorf("active close events = %v; want start then a terminal error", events)
				}
				assertDebugClose(1, readCloses(1)[1])
			}
			if fetches.Load() != 0 {
				t.Errorf("SSE requests = %d; want zero", fetches.Load())
			}
		})
	}
}
