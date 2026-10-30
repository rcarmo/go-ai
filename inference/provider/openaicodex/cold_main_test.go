package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexConcurrentColdDialAdoptionMain(t *testing.T) {
	for _, mode := range []string{"old_first", "new_first", "old_error", "old_cancel", "old_idle"} {
		t.Run(mode, func(t *testing.T) {
			type gate struct {
				ch   chan struct{}
				once sync.Once
			}
			newGate := func() *gate { return &gate{ch: make(chan struct{})} }
			open := func(g *gate) { g.once.Do(func() { close(g.ch) }) }
			allow := []*gate{newGate(), newGate()}
			finish := []*gate{newGate(), newGate()}
			arrived := make(chan int, 4)
			captures := make(chan accountCapture, 8)
			var connections atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				index := int(connections.Add(1)) - 1
				if index < 2 {
					arrived <- index
					select {
					case <-allow[index].ch:
					case <-r.Context().Done():
						return
					}
				}
				conn, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
				defer cancel()
				for n := 0; ; n++ {
					_, data, err := conn.Read(ctx)
					if err != nil {
						return
					}
					var body struct {
						Previous string `json:"previous_response_id"`
					}
					_ = json.Unmarshal(data, &body)
					captures <- accountCapture{account: "acct_123", connection: int32(index + 1), previous: body.Previous}
					if n == 0 && index < 2 {
						select {
						case <-finish[index].ch:
						case <-ctx.Done():
							return
						}
					}
					frame := fmt.Sprintf(`{"type":"response.completed","response":{"id":"r%d","status":"completed","output":[]}}`, index+1)
					if index == 0 && mode == "old_error" {
						frame = `{invalid`
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
			defer func() {
				for _, g := range allow {
					open(g)
				}
				for _, g := range finish {
					open(g)
				}
			}()
			model := codexGrammarModel()
			model.BaseURL = server.URL
			run := func(ctx context.Context) []goai.Event {
				events := []goai.Event{}
				for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: payloadJWT, SessionID: session, Transport: goai.TransportAuto}) {
					events = append(events, event)
				}
				return events
			}
			await := func(ch <-chan int, want int) {
				select {
				case got := <-ch:
					if got != want {
						t.Errorf("dial order %d != %d", got, want)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("cold dial not started")
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
							t.Error("wrong dial response", e.Message)
						}
					}
				}
				if done != 1 {
					t.Error("missing cold completion", done)
				}
			}
			firstCtx, cancelFirst := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancelFirst()
			secondCtx, cancelSecond := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancelSecond()
			firstDone, secondDone := make(chan []goai.Event, 1), make(chan []goai.Event, 1)
			go func() { firstDone <- run(firstCtx) }()
			await(arrived, 0)
			go func() { secondDone <- run(secondCtx) }()
			await(arrived, 1)
			open(allow[0])
			first := nextAccountCapture(t, captures)
			key := codexWebSocketSessionKey{sessionID: session, accountID: "acct_123", wsURL: resolveCodexWSURL(server.URL)}
			codexWebSocketSessionsMu.Lock()
			old := codexWebSocketSessions[key]
			codexWebSocketSessionsMu.Unlock()
			if old == nil {
				t.Fatal("first cold owner absent")
			}
			if mode == "old_idle" {
				open(finish[0])
				checkDone(<-firstDone, "r1")
			}
			open(allow[1])
			second := nextAccountCapture(t, captures)
			codexWebSocketSessionsMu.Lock()
			current := codexWebSocketSessions[key]
			codexWebSocketSessionsMu.Unlock()
			if first.connection != 1 || second.connection != 2 || first.previous != "" || second.previous != "" || current == nil || current == old {
				t.Fatal("last completed cold dial did not adopt", first, second)
			}
			if mode == "old_idle" {
				open(finish[1])
				checkDone(<-secondDone, "r2")
			} else if mode == "new_first" {
				open(finish[1])
				checkDone(<-secondDone, "r2")
				open(finish[0])
				checkDone(<-firstDone, "r1")
			} else {
				if mode == "old_cancel" {
					cancelFirst()
				}
				open(finish[0])
				events := <-firstDone
				if mode == "old_first" {
					checkDone(events, "r1")
				} else {
					failures := 0
					for _, event := range events {
						switch e := event.(type) {
						case *goai.DoneEvent:
							t.Error("failed old owner completed")
						case *goai.ErrorEvent:
							failures++
							if mode == "old_cancel" && e.Reason != goai.StopReasonAborted {
								t.Error(e)
							}
						}
					}
					if failures != 1 {
						t.Error("old owner did not fail once", failures)
					}
				}
				open(finish[1])
				checkDone(<-secondDone, "r2")
			}
			select {
			case <-old.readDone:
			case <-time.After(2 * time.Second):
				t.Error("superseded cold owner reader leaked")
			}
			codexWebSocketSessionsMu.Lock()
			retained := codexWebSocketSessions[key]
			codexWebSocketSessionsMu.Unlock()
			if retained != current {
				t.Fatal("old owner cleanup evicted adopted dial")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			checkDone(run(ctx), "r2")
			followup := nextAccountCapture(t, captures)
			if followup.connection != 2 || followup.previous != "r2" {
				t.Fatal("wrong cold-dial continuation selected", followup)
			}
			stats := GetOpenAICodexWebSocketDebugStats(session)
			if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 {
				t.Error("cold adoption counted as transport failure", stats)
			}
		})
	}
}
