package goai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// --- Azure ---

func TestAdaptReasoningItem(t *testing.T) {
	event := map[string]interface{}{
		"type": "response.output_item.done",
		"item": map[string]interface{}{
			"type": "reasoning",
			"content": []interface{}{
				map[string]interface{}{"type": "reasoning_text", "text": "thinking..."},
			},
		},
	}
	result := goai.NormalizeAzureReasoningEvent(event)
	item, _ := result["item"].(map[string]interface{})
	summary, _ := item["summary"].([]interface{})
	if len(summary) == 0 {
		t.Fatal("expected summary from content adaptation")
	}
}

func TestAdaptCommentaryDone(t *testing.T) {
	event := map[string]interface{}{
		"type": "response.output_item.done",
		"item": map[string]interface{}{
			"type":  "message",
			"phase": "commentary",
			"id":    "item_1",
			"content": []interface{}{
				map[string]interface{}{"type": "output_text", "text": "analysis..."},
			},
		},
	}
	result := goai.NormalizeAzureReasoningEvent(event)
	item, _ := result["item"].(map[string]interface{})
	if item["type"] != "reasoning" {
		t.Fatal("expected type reasoning")
	}
}

func TestNormalizeReasoningTextDone(t *testing.T) {
	event := map[string]interface{}{
		"type": "response.reasoning_text.done",
		"text": "done thinking",
	}
	result := goai.NormalizeAzureReasoningEvent(event)
	if result["type"] != "response.reasoning_summary_part.done" {
		t.Fatal("expected normalized type")
	}
}

// --- Utils ---

func TestShortHash(t *testing.T) {
	h := goai.ShortHash("test string")
	if len(h) != 16 {
		t.Fatalf("expected 16 hex chars, got %d: %s", len(h), h)
	}
	// Deterministic
	if goai.ShortHash("test string") != h {
		t.Fatal("hash should be deterministic")
	}
	// Different input = different hash
	if goai.ShortHash("other") == h {
		t.Fatal("different inputs should differ")
	}
}

func TestCopilotHeaders(t *testing.T) {
	h := goai.CopilotHeaders()
	if h["User-Agent"] == "" {
		t.Fatal("missing User-Agent")
	}
	if len(h) < 4 {
		t.Fatalf("expected at least 4 headers, got %d", len(h))
	}
}

func TestCopilotHeadersWithIntent(t *testing.T) {
	h := goai.CopilotHeadersWithIntent("chat")
	if h["openai-intent"] != "chat" {
		t.Fatal("missing intent header")
	}
}

// --- Logger internals ---

func TestNewStderrLogger(t *testing.T) {
	l := goai.NewStderrLogger(goai.LogLevelWarn)
	l.Debug("should not panic")
	l.Warn("should not panic")
}

// --- Registry ---

func TestClearApiProviders(t *testing.T) {
	goai.RegisterApi(&goai.ApiProvider{Api: "test-clear", Stream: nil, StreamSimple: nil})
	goai.ClearApiProviders()
	if goai.GetApiProvider("test-clear") != nil {
		t.Fatal("should be cleared")
	}
}

func TestClearModels(t *testing.T) {
	goai.RegisterModel(&goai.Model{ID: "test-clear-m", Provider: "test"})
	goai.ClearModels()
	if goai.GetModel("test", "test-clear-m") != nil {
		t.Fatal("should be cleared")
	}
	// Re-register builtins for other tests
	goai.RegisterBuiltinModels()
}

// --- Retry ---

func TestDefaultRetryConfig(t *testing.T) {
	cfg := goai.DefaultRetryConfig()
	if cfg.MaxRetries != 3 {
		t.Fatal("expected 3 retries")
	}
	if cfg.BackoffMultiplier != 2.0 {
		t.Fatal("expected 2.0 multiplier")
	}
}

func TestNoRetryConfig(t *testing.T) {
	cfg := goai.NoRetryConfig()
	if cfg.MaxRetries != 0 {
		t.Fatal("expected 0 retries")
	}
}

func TestNewHTTPClient(t *testing.T) {
	cfg := goai.DefaultRetryConfig()
	client := cfg.NewHTTPClient()
	if client.Timeout != 10*time.Minute {
		t.Fatalf("expected 10m timeout, got %v", client.Timeout)
	}
}

func TestDoWithRetrySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := goai.DoWithRetry(context.Background(), server.Client(), req, goai.DefaultRetryConfig())
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDoWithRetry429(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	cfg := goai.RetryConfig{
		MaxRetries:        5,
		InitialDelay:      time.Millisecond,
		MaxDelay:          10 * time.Millisecond,
		BackoffMultiplier: 1.0,
		ConnectTimeout:    5 * time.Second,
		RequestTimeout:    5 * time.Second,
	}

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := goai.DoWithRetry(context.Background(), server.Client(), req, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 after retries, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestDoWithRetryExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(500)
	}))
	defer server.Close()

	cfg := goai.RetryConfig{
		MaxRetries:        2,
		InitialDelay:      time.Millisecond,
		MaxDelay:          time.Millisecond,
		BackoffMultiplier: 1.0,
		ConnectTimeout:    5 * time.Second,
		RequestTimeout:    5 * time.Second,
	}

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := goai.DoWithRetry(context.Background(), server.Client(), req, cfg)
	if err != nil {
		t.Fatalf("expected final response, got error: %v", err)
	}
	if resp.StatusCode != 500 {
		t.Fatalf("expected final 500 response, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestDoWithRetryOnRetryCallback(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt < 2 {
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	callbackCalled := false
	cfg := goai.RetryConfig{
		MaxRetries:        3,
		InitialDelay:      time.Millisecond,
		MaxDelay:          time.Millisecond,
		BackoffMultiplier: 1.0,
		ConnectTimeout:    5 * time.Second,
		RequestTimeout:    5 * time.Second,
		OnRetry: func(attempt int, delay time.Duration, status int) {
			callbackCalled = true
			if status != 429 {
				t.Errorf("expected status 429, got %d", status)
			}
		},
	}

	req, _ := http.NewRequest("GET", server.URL, nil)
	resp, err := goai.DoWithRetry(context.Background(), server.Client(), req, cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if !callbackCalled {
		t.Fatal("OnRetry callback was not called")
	}
}

// --- Harness ---

func TestAppendAssistantMessage(t *testing.T) {
	ctx := &goai.Context{}
	msg := &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "hi"}}}
	goai.AppendAssistantMessage(ctx, msg)
	if len(ctx.Messages) != 1 || ctx.Messages[0].Role != goai.RoleAssistant {
		t.Fatal("AppendAssistantMessage failed")
	}
}

func TestGetTextContent(t *testing.T) {
	msg := &goai.Message{Content: []goai.ContentBlock{
		{Type: "thinking", Thinking: "hmm"},
		{Type: "text", Text: "hello"},
		{Type: "text", Text: " world"},
	}}
	got := goai.GetTextContent(msg)
	if got != "hello world" {
		t.Fatalf("expected 'hello world', got %q", got)
	}
}

func TestInvokeOnResponse(t *testing.T) {
	called := false
	opts := &goai.StreamOptions{
		OnResponse: func(status int, headers map[string]string, model *goai.Model) {
			called = true
			if status != 200 {
				t.Fatalf("expected 200, got %d", status)
			}
		},
	}

	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"X-Test": {"value"}},
	}
	goai.InvokeOnResponse(opts, resp, &goai.Model{})
	if !called {
		t.Fatal("OnResponse not called")
	}
}

// --- Complete via faux (covers Complete path) ---

func TestCompleteViaFaux(t *testing.T) {
	// Import faux side effect already registered from other tests
	// Register a simple model and provider
	goai.RegisterApi(&goai.ApiProvider{
		Api: "test-complete-api",
		Stream: func(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 3)
			msg := &goai.Message{
				Role:       goai.RoleAssistant,
				Content:    []goai.ContentBlock{{Type: "text", Text: "response"}},
				StopReason: goai.StopReasonStop,
				Usage:      &goai.Usage{Input: 10, Output: 5, TotalTokens: 15},
			}
			ch <- &goai.StartEvent{Partial: msg}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonStop, Message: msg}
			close(ch)
			return ch
		},
		StreamSimple: nil,
	})
	goai.RegisterModel(&goai.Model{ID: "test-complete-model", Provider: "test-complete", Api: "test-complete-api"})

	model := goai.GetModel("test-complete", "test-complete-model")
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}
	msg, err := goai.Complete(context.Background(), model, convCtx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.StopReason != goai.StopReasonStop {
		t.Fatal("expected stop")
	}
}
