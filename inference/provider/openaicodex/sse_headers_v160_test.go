package openaicodex

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// The source row maps openai-codex-stream.test.ts:911–958. The other rows
// separately check the model-only and case-insensitive caller precedence paths.
func TestSourceV160CodexSSEHeaderOverrides(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		modelHeaders, callHeaders map[string]string
		originator, userAgent     string
	}{
		{
			name: "source", modelHeaders: map[string]string{"originator": "my-app"},
			callHeaders: map[string]string{"user-agent": "my-app/1.0", "Authorization": "Bearer ignored"},
			originator:  "my-app", userAgent: "my-app/1.0",
		},
		{
			name: "model-only", modelHeaders: map[string]string{"originator": "model-app", "User-Agent": "model-agent"},
			originator: "model-app", userAgent: "model-agent",
		},
		{
			name:         "caller-over-model",
			modelHeaders: map[string]string{"Originator": "model-app", "User-Agent": "model-agent", "Authorization": "Bearer ignored-model"},
			callHeaders: map[string]string{"oRiGiNaToR": "caller-app", "uSeR-aGeNt": "caller-agent", "authorization": "Bearer ignored-caller",
				"chatgpt-account-id": "ignored-account"},
			originator: "caller-app", userAgent: "caller-agent",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const accountID = "acc_test"
			token := accountJWT(accountID)
			var headers http.Header
			requests := 0
			transport := codexTerminalTransportV160(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if requests != 1 || r.Method != http.MethodPost || r.URL.String() != "https://chatgpt.com/backend-api/codex/responses" {
					return nil, fmt.Errorf("unexpected Codex scripted request %d: %s %s", requests, r.Method, r.URL)
				}
				headers = r.Header.Clone()
				const terminal = `data: {"type":"response.completed","response":{"status":"completed"}}` + "\n\n"
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
					Body: io.NopCloser(strings.NewReader(terminal))}, nil
			})
			model := &goai.Model{ID: "gpt-5.1-codex", Name: "GPT-5.1 Codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
				BaseURL: "https://chatgpt.com/backend-api", Reasoning: true, Input: []string{"text"}, ContextWindow: 400000, MaxTokens: 128000,
				Headers: tc.modelHeaders}
			transcript := &goai.Context{SystemPrompt: "You are a helpful assistant.", Messages: []goai.Message{goai.UserMessage("Say hello")}}
			zero := 0
			result, err := goai.Complete(t.Context(), model, transcript, &goai.StreamOptions{
				APIKey: token, Transport: goai.TransportSSE, Headers: tc.callHeaders, MaxRetries: &zero, HTTPTransport: transport,
			})
			if err != nil || result == nil || result.StopReason != goai.StopReasonStop || requests != 1 {
				t.Fatalf("result = %+v, err=%v requests=%d; want one successful scripted request", result, err, requests)
			}
			if got := headers.Values("Originator"); !slices.Equal(got, []string{tc.originator}) {
				t.Errorf("Originator header = %q, want one %q", got, tc.originator)
			}
			if got := headers.Values("User-Agent"); !slices.Equal(got, []string{tc.userAgent}) {
				t.Errorf("User-Agent header = %q, want one %q", got, tc.userAgent)
			}
			if !slices.Equal(headers.Values("Authorization"), []string{"Bearer " + token}) {
				t.Error("Authorization header did not preserve exactly one synthetic provider Bearer token")
			}
			if !slices.Equal(headers.Values("ChatGPT-Account-Id"), []string{accountID}) || len(headers.Values("X-Api-Key")) != 0 {
				t.Error("expected one token-derived account header and no X-Api-Key")
			}
		})
	}
}
