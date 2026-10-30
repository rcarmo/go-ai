package openaicodex

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/transports/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodexTerminalFailureAutoDoesNotFallbackMain(t *testing.T) {
	var fallback, upgrades atomic.Int32
	started, finished := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			fallback.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"))
			return
		}
		upgrades.Add(1)
		close(started)
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
		err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"failed_response","status":"incomplete","incomplete_details":{"reason":"content_filter"},"end_turn":false,"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`))
		if err != nil {
			t.Error(err)
		}
	}))
	defer func() {
		server.Close()
		select {
		case <-started:
			<-finished
		default:
		}
	}()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	done, failures := 0, 0
	var partial *goai.Message
	session := t.Name()
	ResetOpenAICodexWebSocketDebugStats(session)
	defer ResetOpenAICodexWebSocketDebugStats(session)
	for event := range streamCodex(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{APIKey: jwt, Transport: goai.TransportAuto, SessionID: session}) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			done++
		case *goai.ErrorEvent:
			failures++
			partial = e.Error
			if e.Err == nil || !strings.Contains(e.Err.Error(), "content_filter") {
				t.Error(e.Err)
			}
		}
	}
	if done != 0 || failures != 1 || fallback.Load() != 0 || upgrades.Load() != 1 {
		t.Fatal("failed terminal retried/fell back", done, failures, fallback.Load(), upgrades.Load())
	}
	if partial == nil || partial.StopReason != goai.StopReasonError || partial.ResponseID != "failed_response" || partial.RawStopReason != "incomplete.content_filter" || partial.Usage == nil || partial.Usage.Input != 5 || partial.Usage.Output != 2 || partial.Usage.TotalTokens != 7 || partial.EndTurn == nil || *partial.EndTurn {
		t.Fatal("failed output lost terminal evidence", partial)
	}
	stats := GetOpenAICodexWebSocketDebugStats(session)
	if stats == nil || stats.WebSocketFailures != 0 || stats.SSEFallbacks != 0 || stats.WebSocketFallbackActive || len(partial.Diagnostics) != 0 {
		t.Fatal("semantic failure misclassified as transport", stats, partial.Diagnostics)
	}
}

func TestCodexStartedNormalCloseDoesNotFallbackMain(t *testing.T) {
	var fallback atomic.Int32
	started, finished := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			fallback.Add(1)
			return
		}
		close(started)
		defer close(finished)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "truncated")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if _, _, err = conn.Read(ctx); err != nil {
			t.Error(err)
			return
		}
		if err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_item.added","item":{"type":"message","id":"m","phase":"final_answer"}}`)); err != nil {
			t.Error(err)
		}
	}))
	defer func() {
		server.Close()
		select {
		case <-started:
			<-finished
		default:
		}
	}()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	jwt := "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdF8xMjMifX0."
	done, failures := 0, 0
	session := t.Name()
	ResetOpenAICodexWebSocketDebugStats(session)
	defer ResetOpenAICodexWebSocketDebugStats(session)
	var partial *goai.Message
	for event := range streamCodex(ctx, model, &goai.Context{}, &goai.StreamOptions{APIKey: jwt, Transport: goai.TransportAuto, SessionID: session}) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			done++
		case *goai.ErrorEvent:
			failures++
			partial = e.Error
			if e.Error == nil || e.Error.StopReason != goai.StopReasonError || e.Error.ErrorMessage != "WebSocket stream closed before response.completed" {
				t.Error("truncated output lost partial", e)
			}
		}
	}
	if done != 0 || failures != 1 || fallback.Load() != 0 {
		t.Fatal("started stream fell back", done, failures, fallback.Load())
	}
	stats := GetOpenAICodexWebSocketDebugStats(session)
	if stats == nil || stats.WebSocketFailures != 1 || stats.SSEFallbacks != 0 || !stats.WebSocketFallbackActive {
		t.Fatal("transport failure counters missing", stats)
	}
	if partial == nil || len(partial.Diagnostics) != 1 {
		t.Fatal("transport diagnostic missing", partial)
	}
	diagnostic := partial.Diagnostics[0]
	if diagnostic.Type != "provider_transport_failure" || diagnostic.Details["eventsEmitted"] != true || diagnostic.Details["phase"] != "after_message_stream_start" || diagnostic.Details["configuredTransport"] != "auto" {
		t.Fatal(diagnostic)
	}
	if _, present := diagnostic.Details["fallbackTransport"]; present {
		t.Fatal("false fallback advertised", diagnostic)
	}
	size, ok := diagnostic.Details["requestBytes"].(int)
	if !ok || size <= 0 {
		t.Fatal("request bytes missing", diagnostic)
	}
}
