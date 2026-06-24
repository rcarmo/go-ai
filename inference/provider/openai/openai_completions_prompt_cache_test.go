package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func promptCacheModel(overrides func(*goai.Model)) *goai.Model {
	m := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: "https://api.openai.com/v1", Reasoning: false}
	if overrides != nil {
		overrides(m)
	}
	return m
}

func promptCacheContext() *goai.Context {
	return &goai.Context{SystemPrompt: "sys", Messages: []goai.Message{goai.UserMessage("hi")}}
}

func TestOpenAICompletionsPromptCacheSetsKeyForDirectOpenAIWhenCachingEnabled(t *testing.T) {
	req := buildRequestBody(promptCacheModel(nil), promptCacheContext(), &goai.StreamOptions{SessionID: "session-123"})
	if req.PromptCacheKey != "session-123" || req.PromptCacheRetention != "" {
		t.Fatalf("prompt cache fields = key %q retention %q", req.PromptCacheKey, req.PromptCacheRetention)
	}
}

func TestOpenAICompletionsPromptCacheSets24hRetentionForDirectOpenAIWhenLong(t *testing.T) {
	req := buildRequestBody(promptCacheModel(nil), promptCacheContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionLong, SessionID: "session-456"})
	if req.PromptCacheKey != "session-456" || req.PromptCacheRetention != "24h" {
		t.Fatalf("prompt cache fields = key %q retention %q", req.PromptCacheKey, req.PromptCacheRetention)
	}
}

func TestOpenAICompletionsPromptCacheClampsKeyTo64Characters(t *testing.T) {
	req := buildRequestBody(promptCacheModel(nil), promptCacheContext(), &goai.StreamOptions{SessionID: strings.Repeat("x", 67)})
	if got, want := req.PromptCacheKey, strings.Repeat("x", 64); got != want {
		t.Fatalf("prompt_cache_key=%q, want %q", got, want)
	}
}

func TestOpenAICompletionsPromptCacheOmitsFieldsWhenRetentionNone(t *testing.T) {
	req := buildRequestBody(promptCacheModel(nil), promptCacheContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone, SessionID: "session-789"})
	if req.PromptCacheKey != "" || req.PromptCacheRetention != "" {
		t.Fatalf("prompt cache fields = key %q retention %q, want empty", req.PromptCacheKey, req.PromptCacheRetention)
	}
}

func TestOpenAICompletionsPromptCacheOmitsFieldsForNonOpenAIWithoutLongRetentionCompat(t *testing.T) {
	noLong := false
	model := promptCacheModel(func(m *goai.Model) {
		m.BaseURL = "https://proxy.example.com/v1"
		m.CompletionsCompat = &goai.OpenAICompletionsCompat{SupportsLongCacheRetention: &noLong}
	})
	req := buildRequestBody(model, promptCacheContext(), &goai.StreamOptions{CacheRetention: goai.CacheRetentionLong, SessionID: "session-proxy"})
	if req.PromptCacheKey != "" || req.PromptCacheRetention != "" {
		t.Fatalf("prompt cache fields = key %q retention %q, want empty", req.PromptCacheKey, req.PromptCacheRetention)
	}
}

func TestOpenAICompletionsPromptCacheUsesPICacheRetentionEnvForDirectOpenAI(t *testing.T) {
	t.Setenv("PI_CACHE_RETENTION", "long")
	req := buildRequestBody(promptCacheModel(nil), promptCacheContext(), &goai.StreamOptions{SessionID: "session-env"})
	if req.PromptCacheKey != "session-env" || req.PromptCacheRetention != "24h" {
		t.Fatalf("prompt cache fields = key %q retention %q", req.PromptCacheKey, req.PromptCacheRetention)
	}
}

func TestOpenAICompletionsPromptCacheSendsSessionAffinityHeadersWhenCompatEnabled(t *testing.T) {
	headers := captureOpenAIHeaders(t, &goai.StreamOptions{SessionID: "session-affinity"}, nil)
	if headers.Get("session_id") != "session-affinity" || headers.Get("x-client-request-id") != "session-affinity" || headers.Get("x-session-affinity") != "session-affinity" {
		t.Fatalf("session affinity headers = session_id %q request %q affinity %q", headers.Get("session_id"), headers.Get("x-client-request-id"), headers.Get("x-session-affinity"))
	}
}

func TestOpenAICompletionsPromptCacheOmitsSessionAffinityHeadersWhenRetentionNone(t *testing.T) {
	headers := captureOpenAIHeaders(t, &goai.StreamOptions{CacheRetention: goai.CacheRetentionNone, SessionID: "session-affinity"}, nil)
	if headers.Get("session_id") != "" || headers.Get("x-client-request-id") != "" || headers.Get("x-session-affinity") != "" {
		t.Fatalf("session affinity headers should be omitted when cacheRetention none: %#v", headers)
	}
}

func TestOpenAICompletionsPromptCacheLetsExplicitHeadersOverrideGeneratedSessionAffinityHeaders(t *testing.T) {
	headers := captureOpenAIHeaders(t, &goai.StreamOptions{SessionID: "session-affinity", Headers: map[string]string{"session_id": "override-session", "x-client-request-id": "override-request", "x-session-affinity": "override-affinity"}}, nil)
	if headers.Get("session_id") != "override-session" || headers.Get("x-client-request-id") != "override-request" || headers.Get("x-session-affinity") != "override-affinity" {
		t.Fatalf("session affinity override headers = session_id %q request %q affinity %q", headers.Get("session_id"), headers.Get("x-client-request-id"), headers.Get("x-session-affinity"))
	}
}

func captureOpenAIHeaders(t *testing.T, opts *goai.StreamOptions, mutate func(*goai.Model)) http.Header {
	t.Helper()
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_ = json.NewEncoder(w).Encode(map[string]any{})
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n"))
	}))
	defer server.Close()
	sendAffinity := true
	model := promptCacheModel(func(m *goai.Model) {
		m.BaseURL = server.URL
		m.CompletionsCompat = &goai.OpenAICompletionsCompat{SendSessionAffinityHeaders: &sendAffinity}
		if mutate != nil {
			mutate(m)
		}
	})
	if opts == nil {
		opts = &goai.StreamOptions{}
	}
	opts.APIKey = "test-key"
	for ev := range streamOpenAI(context.Background(), model, promptCacheContext(), opts) {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if got == nil {
		t.Fatal("request not captured")
	}
	return got
}
