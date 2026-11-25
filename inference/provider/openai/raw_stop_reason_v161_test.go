package openai

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.1 packages/ai/test/openai-completions-raw-stop-reason.test.ts.
func TestSourceV161CompletionsRawStopReason(t *testing.T) {
	for _, tc := range []struct {
		name, id, delta, finish, native string
		openRouter                      bool
		stop                            goai.StopReason
		raw, diagnostic                 string
		retry                           bool
		contentType, content            string
	}{
		{"provider-error", "chatcmpl-3", "", "error", "", false, goai.StopReasonError,
			"error", "Provider finish_reason: error", true, "", ""},
		{"content-filter", "chatcmpl-2", "", "content_filter", "", false, goai.StopReasonError,
			"content_filter", "Provider finish_reason: content_filter", false, "", ""},
		{"visible-text", "gen-1", `{"content":"Done."}`, "stop", "MALFORMED_RESPONSE", true, goai.StopReasonStop,
			"stop.MALFORMED_RESPONSE", "", false, "text", "Done."},
		{"reasoning-only", "gen-8", `{"reasoning":"Let me think."}`, "stop", "MALFORMED_RESPONSE", true, goai.StopReasonError,
			"stop.MALFORMED_RESPONSE", "OpenRouter returned an empty response (native_finish_reason: MALFORMED_RESPONSE)", true, "thinking", "Let me think."},
		{"whitespace-other", "gen-9", `{"content":"  "}`, "stop", "OTHER", true, goai.StopReasonError,
			"stop.OTHER", "OpenRouter returned an empty response (native_finish_reason: OTHER)", true, "text", "  "},
		{"clean-stop", "gen-10", "", "stop", "end_turn", true, goai.StopReasonStop,
			"stop.end_turn", "", false, "", ""},
		{"mixed-case-clean-stop", "gen-11", "", "stop", "End_Turn", true, goai.StopReasonStop,
			"stop.End_Turn", "", false, "", ""},
		{"native-provider-error", "gen-3", "", "error", "MALFORMED_FUNCTION_CALL", true, goai.StopReasonError,
			"error.MALFORMED_FUNCTION_CALL", "Provider finish_reason: error (native_finish_reason: MALFORMED_FUNCTION_CALL)", true, "", ""},
		{"non-openrouter", "chatcmpl-6", "", "stop", "MALFORMED_RESPONSE", false, goai.StopReasonStop,
			"stop", "", false, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{ID: "test-model", Name: "Test Model", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions,
				BaseURL: "https://api.openai.com/v1", Input: []string{"text"}, ContextWindow: 128000, MaxTokens: 4096}
			if tc.openRouter {
				model.ID, model.Provider, model.BaseURL = "google/gemini-4-neon", goai.ProviderOpenRouter, "https://openrouter.ai/api/v1"
			}
			var body strings.Builder
			if tc.delta != "" {
				fmt.Fprintf(&body, "data: {\"id\":%q,\"choices\":[{\"index\":0,\"delta\":%s,\"finish_reason\":null}]}\n\n", tc.id, tc.delta)
			}
			native := ""
			if tc.native != "" {
				native = fmt.Sprintf(`,"native_finish_reason":%q`, tc.native)
			}
			fmt.Fprintf(&body, "data: {\"id\":%q,\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":%q%s}]}\n\n", tc.id, tc.finish, native)
			body.WriteString("data: [DONE]\n\n")
			requests := 0
			transport := promptCacheTransportV160(func(r *http.Request) (*http.Response, error) {
				defer r.Body.Close()
				requests++
				if requests != 1 || r.Method != http.MethodPost || r.URL.String() != model.BaseURL+"/chat/completions" || r.Header.Get("Authorization") != "Bearer test" {
					return nil, fmt.Errorf("unexpected scripted request %d: %s %s", requests, r.Method, r.URL)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}, nil
			})
			zero := 0
			message, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}},
				&goai.StreamOptions{APIKey: "test", MaxRetries: &zero, HTTPTransport: transport})
			if err != nil || message == nil || requests != 1 {
				t.Fatalf("result=%+v err=%v requests=%d, want one scripted result", message, err, requests)
			}
			if message.StopReason != tc.stop {
				t.Errorf("stopReason=%q, want %q", message.StopReason, tc.stop)
			}
			if message.RawStopReason != tc.raw {
				t.Errorf("rawStopReason=%q, want %q", message.RawStopReason, tc.raw)
			}
			if message.ErrorMessage != tc.diagnostic {
				t.Errorf("errorMessage=%q, want %q", message.ErrorMessage, tc.diagnostic)
			}
			if got := goai.IsRetryableAssistantError(message); got != tc.retry {
				t.Errorf("isRetryableAssistantError=%v, want %v", got, tc.retry)
			}
			if tc.contentType != "" {
				if len(message.Content) != 1 || message.Content[0].Type != tc.contentType || message.Content[0].Text+message.Content[0].Thinking != tc.content {
					t.Errorf("content=%+v, want %s %q", message.Content, tc.contentType, tc.content)
				}
			}
		})
	}
}
