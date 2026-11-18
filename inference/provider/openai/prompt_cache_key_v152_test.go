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

func TestSourceV152CompletionsPromptCacheKeyPolicy(t *testing.T) {
	t.Setenv("PI_CACHE_RETENTION", "")
	capture := func(t *testing.T, mode string, retention goai.CacheRetention, session string, supportsLong, direct bool) (map[string]any, http.Header) {
		t.Helper()
		var compat goai.OpenAICompletionsCompat
		raw, _ := json.Marshal(map[string]any{"promptCacheKeyMode": mode, "supportsLongCacheRetention": supportsLong})
		if err := json.Unmarshal(raw, &compat); err != nil {
			t.Fatal(err)
		}
		type request struct {
			body   map[string]any
			header http.Header
			err    error
		}
		requests := make(chan request, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var got request
			got.err = json.NewDecoder(r.Body).Decode(&got.body)
			got.header = r.Header.Clone()
			requests <- got
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"id\":\"test\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		}))
		defer server.Close()
		base := server.URL + "/v1"
		if direct {
			base = server.URL + "/api.openai.com/v1"
		}
		model := &goai.Model{ID: "test-model", Api: goai.ApiOpenAICompletions, Provider: goai.ProviderOpenAI, BaseURL: base, MaxTokens: 4096, CompletionsCompat: &compat}
		user := goai.UserMessage("hello")
		user.Timestamp = 1
		env := goai.ProviderEnv(nil)
		if retention == goai.CacheRetentionNone {
			env = goai.ProviderEnv{"PI_CACHE_RETENTION": "long"}
		}
		for event := range streamOpenAI(context.Background(), model, &goai.Context{Messages: []goai.Message{user}}, &goai.StreamOptions{APIKey: "test-key", SessionID: session, CacheRetention: retention, Env: env}) {
			if e, ok := event.(*goai.ErrorEvent); ok {
				t.Fatal(e.Err)
			}
		}
		select {
		case got := <-requests:
			if got.err != nil {
				t.Fatal(got.err)
			}
			return got.body, got.header
		default:
			t.Fatal("no HTTP request")
		}
		return nil, nil
	}
	assertField := func(t *testing.T, payload map[string]any, field, want string) {
		t.Helper()
		value, present := payload[field]
		if present != (want != "") || (present && value != want) {
			t.Fatalf("%s = %v (present %v), want %q", field, value, present, want)
		}
	}
	for _, retention := range []goai.CacheRetention{"", goai.CacheRetentionShort, goai.CacheRetentionLong} {
		t.Run("stable/"+string(retention), func(t *testing.T) {
			first, header := capture(t, "enabled", retention, "session-proxy", false, false)
			repeat, _ := capture(t, "enabled", retention, "session-proxy", false, false)
			other, _ := capture(t, "enabled", retention, "other-session", false, false)
			assertField(t, first, "prompt_cache_key", "session-proxy")
			assertField(t, repeat, "prompt_cache_key", "session-proxy")
			assertField(t, other, "prompt_cache_key", "other-session")
			assertField(t, first, "prompt_cache_retention", "")
			if key, exists := header[http.CanonicalHeaderKey("session_id")]; exists {
				t.Fatal("unexpected affinity", key)
			}
		})
	}
	for _, tc := range []struct {
		name, mode string
		retention  goai.CacheRetention
		session    string
		direct     bool
		want, ttl  string
	}{
		{"default proxy short", "", goai.CacheRetentionShort, "session-proxy", false, "", ""},
		{"default proxy long", "", goai.CacheRetentionLong, "session-proxy", false, "session-proxy", "24h"},
		{"auto proxy short", "auto", goai.CacheRetentionShort, "session-proxy", false, "", ""},
		{"auto proxy long", "auto", goai.CacheRetentionLong, "session-proxy", false, "session-proxy", "24h"},
		{"none overrides env", "enabled", goai.CacheRetentionNone, "session-proxy", false, "", ""},
		{"disabled direct short", "disabled", goai.CacheRetentionShort, "session-disabled", true, "", ""},
		{"disabled direct long", "disabled", goai.CacheRetentionLong, "session-disabled", true, "", "24h"},
		{"disabled proxy long", "disabled", goai.CacheRetentionLong, "session-proxy", false, "", "24h"},
		{"no invented identity", "enabled", goai.CacheRetentionShort, "", false, "", ""},
		{"Unicode clamp", "enabled", goai.CacheRetentionShort, strings.Repeat("𐐀", 67), false, strings.Repeat("𐐀", 64), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := capture(t, tc.mode, tc.retention, tc.session, true, tc.direct)
			assertField(t, payload, "prompt_cache_key", tc.want)
			assertField(t, payload, "prompt_cache_retention", tc.ttl)
		})
	}
}
