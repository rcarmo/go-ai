package openaicodex

import (
	"context"
	"encoding/base64"
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

type accountCapture struct {
	account    string
	connection int32
	previous   string
}

func accountJWT(account string) string {
	data, _ := json.Marshal(map[string]interface{}{"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account}})
	return "eyJhbGciOiJub25lIn0." + base64.RawURLEncoding.EncodeToString(data) + "."
}
func accountServer(t *testing.T, barrier ...<-chan struct{}) (*httptest.Server, <-chan accountCapture) {
	t.Helper()
	captures := make(chan accountCapture, 32)
	var connections atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		number := connections.Add(1)
		account := r.Header.Get("ChatGPT-Account-Id")
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var body struct {
				Previous string `json:"previous_response_id"`
			}
			if err = json.Unmarshal(data, &body); err != nil {
				t.Error(err)
				return
			}
			captures <- accountCapture{account, number, body.Previous}
			if len(barrier) > 0 {
				select {
				case <-barrier[0]:
				case <-ctx.Done():
					return
				}
			}
			frame := fmt.Sprintf(`{"type":"response.completed","response":{"id":%q,"status":"completed","output":[]}}`, "r_"+account)
			if err = conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
				return
			}
		}
	}))
	return server, captures
}
func requestAccount(t *testing.T, model *goai.Model, session, account string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := 0
	for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: accountJWT(account), SessionID: session, Transport: goai.TransportAuto}) {
		switch e := event.(type) {
		case *goai.ErrorEvent:
			t.Error(e.Err)
		case *goai.DoneEvent:
			done++
			if e.Message.ResponseID != "r_"+account {
				t.Error("wrong account response", e.Message)
			}
		}
	}
	if done != 1 {
		t.Error("account request did not complete", account, done)
	}
}
func nextAccountCapture(t *testing.T, captures <-chan accountCapture) accountCapture {
	t.Helper()
	select {
	case result := <-captures:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("missing account request")
		return accountCapture{}
	}
}

func TestCodexAccountPartitionContinuationAndEvictionMain(t *testing.T) {
	server, captures := accountServer(t)
	defer server.Close()
	session := t.Name()
	defer ResetOpenAICodexWebSocketDebugStats(session)
	defer CloseOpenAICodexWebSocketSessions(session)
	model := codexGrammarModel()
	model.BaseURL = server.URL
	requestAccount(t, model, session, "A")
	first := nextAccountCapture(t, captures)
	requestAccount(t, model, session, "B")
	second := nextAccountCapture(t, captures)
	requestAccount(t, model, session, "A")
	third := nextAccountCapture(t, captures)
	if first.account != "A" || second.account != "B" || third.account != "A" || first.connection == second.connection || third.connection != first.connection || first.previous != "" || second.previous != "" || third.previous != "r_A" {
		t.Fatal("account socket/continuation crossed boundary", first, second, third)
	}
	keyA := codexWebSocketSessionKey{sessionID: session, accountID: "A", wsURL: resolveCodexWSURL(server.URL)}
	keyB := codexWebSocketSessionKey{sessionID: session, accountID: "B", wsURL: resolveCodexWSURL(server.URL)}
	codexWebSocketSessionsMu.Lock()
	oldA, entryB := codexWebSocketSessions[keyA], codexWebSocketSessions[keyB]
	codexWebSocketSessionsMu.Unlock()
	if oldA == nil || entryB == nil {
		t.Fatal("missing account partitions")
	}
	removeCodexWebSocketSession(session, oldA)
	requestAccount(t, model, session, "A")
	freshA := nextAccountCapture(t, captures)
	if freshA.connection == first.connection || freshA.previous != "" {
		t.Fatal("evicted account retained stale continuation", freshA)
	}
	removeCodexWebSocketSession(session, oldA)
	scheduleCodexWebSocketIdleClose(session, oldA)
	codexWebSocketSessionsMu.Lock()
	newA, stillB := codexWebSocketSessions[keyA], codexWebSocketSessions[keyB]
	codexWebSocketSessionsMu.Unlock()
	if newA == nil || newA == oldA || stillB != entryB {
		t.Fatal("stale account eviction affected another entry")
	}
	requestAccount(t, model, session, "B")
	reusedB := nextAccountCapture(t, captures)
	if reusedB.connection != second.connection || reusedB.account != "B" || reusedB.previous != "r_B" {
		t.Fatal("A eviction damaged B continuation", reusedB)
	}
	CloseOpenAICodexWebSocketSessions(session)
	codexWebSocketSessionsMu.Lock()
	for key := range codexWebSocketSessions {
		if key.sessionID == session {
			t.Error("logical session close left an account partition", key)
		}
	}
	codexWebSocketSessionsMu.Unlock()
	requestAccount(t, model, session, "B")
	closedB := nextAccountCapture(t, captures)
	if closedB.connection == second.connection || closedB.previous != "" {
		t.Fatal("logical session close retained B", closedB)
	}
}

func TestCodexSameAccountDifferentOriginMain(t *testing.T) {
	one, firstCaptures := accountServer(t)
	defer one.Close()
	two, secondCaptures := accountServer(t)
	defer two.Close()
	session := t.Name()
	defer ResetOpenAICodexWebSocketDebugStats(session)
	defer CloseOpenAICodexWebSocketSessions(session)
	model := codexGrammarModel()
	model.BaseURL = one.URL
	requestAccount(t, model, session, "A")
	first := nextAccountCapture(t, firstCaptures)
	model.BaseURL = two.URL
	requestAccount(t, model, session, "A")
	second := nextAccountCapture(t, secondCaptures)
	model.BaseURL = one.URL
	requestAccount(t, model, session, "A")
	third := nextAccountCapture(t, firstCaptures)
	if first.previous != "" || second.previous != "" || third.previous != "r_A" || third.connection != first.connection {
		t.Fatal("origin cache/continuation boundary failed", first, second, third)
	}
}

func TestCodexConcurrentAccountsMain(t *testing.T) {
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	server, captures := accountServer(t, gate)
	session := t.Name()
	var jobs sync.WaitGroup
	defer func() {
		release()
		jobs.Wait()
		CloseOpenAICodexWebSocketSessions(session)
		ResetOpenAICodexWebSocketDebugStats(session)
		server.Close()
	}()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	for _, account := range []string{"A", "B"} {
		jobs.Add(1)
		go func(account string) { defer jobs.Done(); requestAccount(t, model, session, account) }(account)
	}
	first, second := nextAccountCapture(t, captures), nextAccountCapture(t, captures)
	if first.account == second.account || first.connection == second.connection || first.previous != "" || second.previous != "" {
		t.Error("concurrent accounts shared a cached connection", first, second)
	}
	release()
	jobs.Wait()
}
