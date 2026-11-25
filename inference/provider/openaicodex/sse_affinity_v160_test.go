package openaicodex

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 openai-codex-stream.test.ts:657–760,762–811,863–909.
// Compare only affinity fields on the decoded wire, not request defaults or
// ancillary GitHub prompt bootstrap in the JS fixture.
func TestSourceV160CodexSSESessionAffinity(t *testing.T) {
	for _, tc := range []struct {
		name, session, want string
		retention           goai.CacheRetention
	}{
		{"provided", "test-session-123", "test-session-123", ""},
		{"none", "one-off-summary", "", goai.CacheRetentionNone},
		{"clamped", strings.Repeat("x", 67), strings.Repeat("x", 64), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := accountJWT("acc_test")
			var headers http.Header
			var body map[string]any
			requests := 0
			transport := codexTerminalTransportV160(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if requests != 1 || r.Method != http.MethodPost || r.URL.String() != "https://chatgpt.com/backend-api/codex/responses" {
					return nil, fmt.Errorf("unexpected Codex scripted request %d: %s %s", requests, r.Method, r.URL)
				}
				headers = r.Header.Clone()
				var reader io.Reader = r.Body
				switch encoding := headers.Get("Content-Encoding"); encoding {
				case "zstd":
					decoder, err := zstd.NewReader(reader)
					if err != nil {
						return nil, err
					}
					defer decoder.Close()
					reader = decoder
				case "":
				default:
					return nil, fmt.Errorf("unexpected Codex request encoding %q", encoding)
				}
				if err := json.NewDecoder(reader).Decode(&body); err != nil {
					return nil, err
				}
				const terminal = `data: {"type":"response.completed","response":{"status":"completed"}}` + "\n\n"
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader(terminal))}, nil
			})
			model := &goai.Model{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
				BaseURL: "https://chatgpt.com/backend-api", Reasoning: true, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000}
			transcript := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Say hello")}}
			zero := 0
			result, err := goai.Complete(t.Context(), model, transcript, &goai.StreamOptions{APIKey: token, Transport: goai.TransportSSE, MaxRetries: &zero,
				SessionID: tc.session, CacheRetention: tc.retention, HTTPTransport: transport, Env: goai.ProviderEnv{"PI_CACHE_RETENTION": ""}})
			if err != nil || result == nil || result.StopReason != goai.StopReasonStop || requests != 1 || body == nil {
				t.Fatalf("result = %+v, err=%v requests=%d; want one successful scripted request", result, err, requests)
			}
			if !slices.Equal(headers.Values("Authorization"), []string{"Bearer " + token}) ||
				!slices.Equal(headers.Values("ChatGPT-Account-Id"), []string{"acc_test"}) || len(headers.Values("X-Api-Key")) != 0 {
				t.Error("unexpected synthetic Codex authentication")
			}
			var wantHeader []string
			wantAffinity := map[string]any{}
			if tc.want != "" {
				wantHeader = []string{tc.want}
				wantAffinity["prompt_cache_key"] = tc.want
			}
			for _, name := range []string{"session-id", "x-client-request-id"} {
				if got := headers.Values(name); !slices.Equal(got, wantHeader) {
					t.Errorf("%s header = %q, want %q", name, got, wantHeader)
				}
			}
			if len(headers.Values("session_id")) != 0 {
				t.Error("Codex must omit the legacy session_id header")
			}
			affinity := map[string]any{}
			for key, value := range body {
				if strings.HasPrefix(key, "prompt_cache_") {
					affinity[key] = value
				}
			}
			if !reflect.DeepEqual(affinity, wantAffinity) {
				t.Errorf("cache affinity fields = %#v, want %#v", affinity, wantAffinity)
			}
		})
	}
}
