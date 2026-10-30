package openaicodex

import (
	"context"
	"encoding/json"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type busyCapture struct {
	connection int32
	previous   string
	store      bool
	inputs     int
}

func TestCodexBusySessionSpareOwnershipMain(t *testing.T) {
	for _, mode := range []string{"success", "cancel", "observer_error", "protocol_error", "owner_cancel"} {
		t.Run(mode, func(t *testing.T) {
			ownerGate := make(chan struct{})
			var gateOnce sync.Once
			releaseOwner := func() { gateOnce.Do(func() { close(ownerGate) }) }
			defer releaseOwner()
			captures := make(chan busyCapture, 8)
			spareClosed := make(chan struct{})
			var connections atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				number := connections.Add(1)
				if number == 2 {
					defer close(spareClosed)
				}
				ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
				defer cancel()
				for n := 0; ; n++ {
					_, data, err := conn.Read(ctx)
					if err != nil {
						return
					}
					var body struct {
						Previous string        `json:"previous_response_id"`
						Store    bool          `json:"store"`
						Input    []interface{} `json:"input"`
					}
					if err = json.Unmarshal(data, &body); err != nil {
						t.Error(err)
						return
					}
					captures <- busyCapture{number, body.Previous, body.Store, len(body.Input)}
					id := "warm"
					if number == 1 && n == 1 {
						_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"owner"}}`))
						select {
						case <-ownerGate:
						case <-ctx.Done():
							return
						}
						id = "owner"
					} else if number == 2 {
						id = "spare"
						if mode != "success" && mode != "owner_cancel" {
							_ = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"spare"}}`))
							if mode == "protocol_error" {
								_ = conn.Write(ctx, websocket.MessageText, []byte(`{invalid`))
							}
							continue
						}
					}
					frame, _ := json.Marshal(map[string]interface{}{"type": "response.completed", "response": map[string]interface{}{"id": id, "status": "completed", "output": []interface{}{}}})
					if err = conn.Write(ctx, websocket.MessageText, frame); err != nil {
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
			run := func(ctx context.Context, hook func(interface{}, *goai.Model) error) []goai.Event {
				events := []goai.Event{}
				for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportAuto, SessionID: session, OnProviderStreamEvent: hook}) {
					events = append(events, event)
				}
				return events
			}
			next := func() busyCapture {
				select {
				case value := <-captures:
					return value
				case <-time.After(3 * time.Second):
					releaseOwner()
					t.Fatal("spare serialized behind busy owner")
					return busyCapture{}
				}
			}
			checkDone := func(events []goai.Event, id string) {
				t.Helper()
				done := 0
				for _, event := range events {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						t.Error(e.Err)
					case *goai.DoneEvent:
						done++
						if e.Message.ResponseID != id {
							t.Error(e.Message)
						}
					}
				}
				if done != 1 {
					t.Error("missing completion", done, id)
				}
			}
			checkDone(run(context.Background(), nil), "warm")
			warm := next()
			key := codexWebSocketSessionKey{sessionID: session, accountID: "acct_123", wsURL: resolveCodexWSURL(server.URL)}
			codexWebSocketSessionsMu.Lock()
			entry := codexWebSocketSessions[key]
			codexWebSocketSessionsMu.Unlock()
			if entry == nil {
				t.Fatal("warm connection not cached")
			}
			ownerCtx, cancelOwner := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancelOwner()
			ownerDone := make(chan []goai.Event, 1)
			go func() { ownerDone <- run(ownerCtx, nil) }()
			owner := next()
			spareCtx, cancelSpare := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelSpare()
			sentinel := errors.New("spare observer error")
			hook := func(interface{}, *goai.Model) error {
				if mode == "owner_cancel" {
					cancelOwner()
				}
				if mode == "cancel" {
					cancelSpare()
				}
				if mode == "observer_error" {
					return sentinel
				}
				return nil
			}
			spareEvents := run(spareCtx, hook)
			spare := next()
			if warm.connection != owner.connection || owner.previous != "warm" || owner.store || spare.connection == owner.connection || spare.previous != "" || spare.store || spare.inputs != 1 {
				t.Error("spare reused cached input/continuation", warm, owner, spare)
			}
			if mode == "success" || mode == "owner_cancel" {
				checkDone(spareEvents, "spare")
			} else {
				failures := 0
				for _, event := range spareEvents {
					switch e := event.(type) {
					case *goai.DoneEvent:
						t.Error("failed spare completed")
					case *goai.ErrorEvent:
						failures++
						if mode == "cancel" && e.Reason != goai.StopReasonAborted {
							t.Error(e)
						}
						if mode == "observer_error" && !errors.Is(e.Err, sentinel) {
							t.Error(e)
						}
					}
				}
				if failures != 1 {
					t.Error("spare did not fail once", failures)
				}
			}
			select {
			case <-spareClosed:
			case <-time.After(2 * time.Second):
				t.Error("transient spare socket not closed")
			}
			if mode == "owner_cancel" {
				releaseOwner()
				ownerEvents := <-ownerDone
				aborted := 0
				for _, event := range ownerEvents {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						if e.Reason == goai.StopReasonAborted {
							aborted++
						} else {
							t.Error(e)
						}
					case *goai.DoneEvent:
						t.Error("cancelled cached owner completed")
					}
				}
				if aborted != 1 {
					t.Error("owner cancellation lost", aborted)
				}
				codexWebSocketSessionsMu.Lock()
				current := codexWebSocketSessions[key]
				codexWebSocketSessionsMu.Unlock()
				if current != nil {
					t.Error("spare was promoted into cancelled owner cache")
				}
				checkDone(run(context.Background(), nil), "warm")
				fresh := next()
				if fresh.connection == owner.connection || fresh.connection == spare.connection || fresh.previous != "" {
					t.Error("cancelled owner/spare continuation reused", fresh)
				}
				stats := GetOpenAICodexWebSocketDebugStats(session)
				if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 {
					t.Error("owner cancellation counted as transport failure", stats)
				}
				return
			}

			codexWebSocketSessionsMu.Lock()
			current := codexWebSocketSessions[key]
			busy := entry.busy
			codexWebSocketSessionsMu.Unlock()
			if current != entry || !busy {
				t.Error("spare release altered busy cached owner")
			}
			releaseOwner()
			checkDone(<-ownerDone, "owner")
			checkDone(run(context.Background(), nil), "warm")
			after := next()
			if after.connection != owner.connection || after.previous != "owner" {
				t.Error("spare overwrote cached continuation", after)
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 || stats.CachedContextRequests != 3 || stats.FullContextRequests < 2 {
				t.Error("spare accounting/failure classification differs", stats)
			}
		})
	}
}

func TestCodexIdleExpiryOwnershipMain(t *testing.T) {
	key := codexWebSocketSessionKey{sessionID: t.Name(), accountID: "A", wsURL: "ws://fixture"}
	old, fresh := time.NewTimer(time.Hour), time.NewTimer(time.Hour)
	defer old.Stop()
	defer fresh.Stop()
	defer CloseOpenAICodexWebSocketSessions(key.sessionID)
	entry := &codexWebSocketSessionEntry{key: key, idleTimer: fresh}
	codexWebSocketSessionsMu.Lock()
	codexWebSocketSessions[key] = entry
	codexWebSocketSessionsMu.Unlock()
	expireCodexWebSocketSession(key, entry, old)
	codexWebSocketSessionsMu.Lock()
	if codexWebSocketSessions[key] != entry {
		t.Error("stale timer expired newly idle owner")
	}
	entry.busy = true
	codexWebSocketSessionsMu.Unlock()
	expireCodexWebSocketSession(key, entry, fresh)
	codexWebSocketSessionsMu.Lock()
	if codexWebSocketSessions[key] != entry {
		t.Error("idle timer expired busy owner")
	}
	replacement := &codexWebSocketSessionEntry{key: key, idleTimer: fresh}
	codexWebSocketSessions[key] = replacement
	entry.busy = false
	codexWebSocketSessionsMu.Unlock()
	expireCodexWebSocketSession(key, entry, fresh)
	codexWebSocketSessionsMu.Lock()
	if codexWebSocketSessions[key] != replacement {
		t.Error("old entry expired replacement")
	}
	codexWebSocketSessionsMu.Unlock()
	expireCodexWebSocketSession(key, replacement, fresh)
	codexWebSocketSessionsMu.Lock()
	if codexWebSocketSessions[key] != nil {
		t.Error("matching idle owner did not expire")
	}
	codexWebSocketSessionsMu.Unlock()
}
