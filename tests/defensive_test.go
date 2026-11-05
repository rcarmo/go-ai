package goai_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestDoWithRetryNegativeMaxRetriesClampsToSingleAttempt(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := http.NewRequest("GET", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := goai.DoWithRetry(context.Background(), &http.Client{Timeout: time.Second}, req, goai.RetryConfig{MaxRetries: -1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
	if attempts != 1 {
		t.Fatalf("expected one attempt when MaxRetries < 0, got %d", attempts)
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

// A very large finite server delay must hit the configured cap, not wrap into
// a negative duration and become a short fallback retry.
func TestDoWithRetryHugeServerDelayIsCapped(t *testing.T) {
	calls := 0
	closed := false
	client := &http.Client{Transport: providerRetryTransportMain(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"1e100"}}, Body: &retryCloseWitness{Reader: strings.NewReader("busy"), closed: &closed}}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
	cfg := goai.NoRetryConfig()
	cfg.MaxRetries, cfg.MaxRetryDelayMs = 1, 1000
	cfg.InitialDelay = time.Millisecond
	resp, err := goai.DoWithRetry(ctx, client, req, cfg)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "exceeds cap") || calls != 1 || !closed {
		t.Fatalf("response=%v err=%v calls=%d closed=%v", resp, err, calls, closed)
	}
}

type retryCloseWitness struct {
	io.Reader
	closed *bool
}

func (b *retryCloseWitness) Close() error { *b.closed = true; return nil }

func TestDoWithRetryHugeConfiguredCapDoesNotWrap(t *testing.T) {
	maxDurationMs := int64((1<<63 - 1) / int64(time.Millisecond))
	if int64(int(maxDurationMs+1)) != maxDurationMs+1 {
		t.Skip("int too narrow to exercise duration overflow")
	}
	calls := 0
	client := &http.Client{Transport: providerRetryTransportMain(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"0.001"}}, Body: io.NopCloser(strings.NewReader("busy"))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
	cfg := goai.NoRetryConfig()
	cfg.MaxRetries, cfg.MaxRetryDelayMs = 1, int(maxDurationMs+1)
	response, err := goai.DoWithRetry(ctx, client, req, cfg)
	if response != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil || response.StatusCode != 200 || calls != 2 {
		t.Fatalf("response=%v err=%v calls=%d", response, err, calls)
	}
}
