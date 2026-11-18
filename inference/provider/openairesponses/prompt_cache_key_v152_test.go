package openairesponses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// openai-responses-prompt-cache.test.ts: inspect the encoded request sent by
// the real streaming provider, not a request builder or affinity header.
func TestSourceV152ResponsesPromptCacheKeyPolicy(t *testing.T) {
	t.Setenv("PI_CACHE_RETENTION", "")
	for _, tc := range []struct {
		name, mode string
		retention  goai.CacheRetention
		want       bool
	}{
		{name: "default", want: true},
		{name: "enabled", mode: "enabled", want: true},
		{name: "disabled", mode: "disabled"},
		{name: "none overrides enabled", mode: "enabled", retention: goai.CacheRetentionNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var compat goai.OpenAIResponsesCompat
			raw, _ := json.Marshal(map[string]string{"promptCacheKeyMode": tc.mode})
			if err := json.Unmarshal(raw, &compat); err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			seen := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen <- json.NewDecoder(r.Body).Decode(&payload)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2,\"input_tokens_details\":{\"cached_tokens\":0}}}}\n\n"))
			}))
			defer server.Close()
			model := &goai.Model{ID: "test-model", Api: goai.ApiOpenAIResponses, Provider: "test-proxy", BaseURL: server.URL + "/v1", MaxTokens: 4096, ResponsesCompat: &compat}
			user := goai.UserMessage("hello")
			user.Timestamp = 1
			for event := range streamResponses(context.Background(), model, &goai.Context{Messages: []goai.Message{user}}, &goai.StreamOptions{APIKey: "test-key", SessionID: "session-123", CacheRetention: tc.retention}) {
				if e, ok := event.(*goai.ErrorEvent); ok {
					t.Fatal(e.Err)
				}
			}
			select {
			case err := <-seen:
				if err != nil {
					t.Fatal(err)
				}
			default:
				t.Fatal("no HTTP request")
			}
			key, present := payload["prompt_cache_key"]
			if present != tc.want || (present && key != "session-123") {
				t.Fatalf("key = %v (present %v), want presence %v", key, present, tc.want)
			}
		})
	}
}
