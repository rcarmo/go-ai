package openaicodex

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type cancelAtEOF struct {
	cancel context.CancelFunc
	closed bool
}

func (r *cancelAtEOF) Read([]byte) (int, error) { r.cancel(); return 0, io.EOF }
func (r *cancelAtEOF) Close() error             { r.closed = true; return nil }

func TestCodexSSECancellationAtEOFMain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelAtEOF{cancel: cancel}
	failures := 0
	processCodexSSEWithOptions(ctx, reader, codexGrammarModel(), nil, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			t.Error("cancelled EOF completed")
		case *goai.ErrorEvent:
			failures++
			if e.Reason != goai.StopReasonAborted || !errors.Is(e.Err, context.Canceled) || e.Error.StopReason != goai.StopReasonAborted {
				t.Error("cancelled EOF became protocol error", e)
			}
		}
	}, nil, codexToolView{})
	if failures != 1 || reader.closed {
		t.Fatal("parser failed cancellation/reader ownership", failures, reader.closed)
	}
}

func TestCodexSSETerminalClosesWithoutEOFMain(t *testing.T) {
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(closed)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"terminal\",\"status\":\"completed\"}}\n\n"+strings.Repeat("data: {invalid\n\n", 100))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model := codexGrammarModel()
	model.BaseURL = server.URL
	seen, done := 0, 0
	for event := range streamCodex(ctx, model, &goai.Context{}, &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportSSE, OnProviderStreamEvent: func(any, *goai.Model) error { seen++; return nil }}) {
		switch e := event.(type) {
		case *goai.DoneEvent:
			done++
			if e.Message.ResponseID != "terminal" {
				t.Error(e.Message)
			}
		case *goai.ErrorEvent:
			t.Error("post-terminal data processed or waited for EOF", e.Err)
		}
	}
	if seen != 1 || done != 1 {
		t.Fatal("terminal did not end SSE adapter", seen, done)
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP body not released after terminal")
	}
}
