package goai_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestStreamNilModel(t *testing.T) {
	events := goai.Stream(context.Background(), nil, &goai.Context{}, nil)
	event := <-events
	err, ok := event.(*goai.ErrorEvent)
	if !ok {
		t.Fatalf("expected ErrorEvent, got %T", event)
	}
	if err.Err == nil || err.Err.Error() != "nil model" {
		t.Fatalf("unexpected error: %v", err.Err)
	}
}

func TestAppendAssistantMessageNilSafe(t *testing.T) {
	ctx := &goai.Context{}
	goai.AppendAssistantMessage(ctx, nil)
	if len(ctx.Messages) != 0 {
		t.Fatal("nil assistant message should be ignored")
	}
	if goai.GetTextContent(nil) != "" {
		t.Fatal("GetTextContent(nil) should be empty")
	}
	if goai.HasToolCalls(nil) {
		t.Fatal("HasToolCalls(nil) should be false")
	}
	if goai.NeedsToolExecution(nil) {
		t.Fatal("NeedsToolExecution(nil) should be false")
	}
	if got := goai.GetToolCalls(nil); got != nil {
		t.Fatalf("GetToolCalls(nil) = %#v, want nil", got)
	}
}

func TestDoWithRetryRequiresReplayableBody(t *testing.T) {
	req, err := http.NewRequest("POST", "http://example.com", io.NopCloser(bytes.NewBufferString("x")))
	if err != nil {
		t.Fatal(err)
	}
	// Force nil GetBody so retries would be unsafe.
	req.GetBody = nil
	_, err = goai.DoWithRetry(context.Background(), &http.Client{Timeout: time.Second}, req, goai.RetryConfig{MaxRetries: 1})
	if err == nil {
		t.Fatal("expected replayable-body error")
	}
}

func TestDoWithRetryReplaysBodyAcrossRetries(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body, _ := io.ReadAll(r.Body)
		if string(body) != "payload" {
			t.Fatalf("attempt %d: body=%q", attempts, string(body))
		}
		if attempts == 1 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	req, err := http.NewRequest("POST", server.URL, bytes.NewBufferString("payload"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := goai.RetryConfig{MaxRetries: 1, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond, BackoffMultiplier: 1, ConnectTimeout: time.Second, RequestTimeout: time.Second}
	resp, err := goai.DoWithRetry(context.Background(), &http.Client{Timeout: time.Second}, req, cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}
