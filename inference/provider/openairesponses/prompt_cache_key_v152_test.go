package openairesponses

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type promptCacheFetch func(*http.Request) (*http.Response, error)

func (f promptCacheFetch) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Unchanged in pi v1.6.0: packages/ai/test/openai-responses-prompt-cache.test.ts:57-71.
// Inspect the serialized request at the transport boundary, as the original
// fetch stub does, without substituting a different proxy authority.
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
			compatJSON := `{}` // The default case has no defined mode.
			if tc.mode != "" {
				compatJSON = fmt.Sprintf(`{"promptCacheKeyMode":%q}`, tc.mode)
			}
			var compat goai.OpenAIResponsesCompat
			if err := json.Unmarshal([]byte(compatJSON), &compat); err != nil {
				t.Fatal(err)
			}
			model := &goai.Model{
				ID: "test-model", Name: "Test Model", Api: goai.ApiOpenAIResponses, Provider: "test-proxy",
				BaseURL: "https://proxy.example.com/v1", Input: []string{"text"}, ContextWindow: 128000,
				MaxTokens: 4096, ResponsesCompat: &compat,
			}
			var payload map[string]any
			var requests, completions int
			transport := promptCacheFetch(func(r *http.Request) (*http.Response, error) {
				requests++
				defer r.Body.Close()
				if r.Method != http.MethodPost || r.URL.String() != "https://proxy.example.com/v1/responses" {
					return nil, fmt.Errorf("unexpected proxy request: %s %s", r.Method, r.URL)
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					return nil, err
				}
				const completed = `data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2,"input_tokens_details":{"cached_tokens":0}}}}` + "\n\n"
				return &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader(completed)), Request: r,
				}, nil
			})
			user := goai.UserMessage("hello")
			user.Timestamp = 1
			opts := &goai.StreamOptions{APIKey: "test-key", SessionID: "session-123", CacheRetention: tc.retention, HTTPTransport: transport}
			for event := range goai.Stream(t.Context(), model, &goai.Context{Messages: []goai.Message{user}}, opts) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					t.Fatalf("unexpected stream error: %v", e.Err)
				case *goai.DoneEvent:
					completions++
				}
			}
			if requests != 1 || completions != 1 || payload == nil {
				t.Fatalf("want one captured JSON request and completion; got requests=%d completions=%d body=%v", requests, completions, payload)
			}
			key, present := payload["prompt_cache_key"]
			if present != tc.want || (present && key != "session-123") {
				t.Fatalf("key = %v (present %v), want presence %v", key, present, tc.want)
			}
		})
	}
}
