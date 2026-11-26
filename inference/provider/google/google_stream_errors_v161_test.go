package google

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.1 google-stream-errors.test.ts:42–80. Extra cases
// isolate chunk counts, first block feedback and terminal precedence.
func TestSourceV161GoogleStreamErrors(t *testing.T) {
	for _, adapter := range []struct {
		name, label, path, block string
		api                      goai.Api
	}{
		{"genai", "Google", "/v1/models/", "", goai.ApiGoogleGenerativeAI},
		{"vertex", "Google Vertex", "/v1/publishers/google/models/", `,"blockReasonMessage":"Blocked by policy"`, goai.ApiGoogleVertex},
	} {
		blocked := `{"promptFeedback":{"blockReason":"PROHIBITED_CONTENT","safetyRatings":[{"category":"HARM_CATEGORY_SEXUALLY_EXPLICIT","probability":"HIGH"}]` + adapter.block + `}}`
		blockDiagnostic := adapter.label + " prompt blocked: PROHIBITED_CONTENT. "
		if adapter.api == goai.ApiGoogleVertex {
			blockDiagnostic += "Blocked by policy. "
		}
		blockDiagnostic += "Safety rating: HARM_CATEGORY_SEXUALLY_EXPLICIT (HIGH)"
		emptyPrefix := adapter.label + " stream ended without a finish reason after receiving "
		for _, tc := range []struct {
			name, diagnostic string
			chunks           []string
			stop             goai.StopReason
			retry            bool
		}{
			{"blocked", blockDiagnostic, []string{blocked}, goai.StopReasonError, false},
			{"empty", emptyPrefix + "0 chunks", nil, goai.StopReasonError, true},
			{"one-chunk", emptyPrefix + "1 chunk", []string{`{}`}, goai.StopReasonError, true},
			{"two-chunks", emptyPrefix + "2 chunks", []string{`{}`, `{}`}, goai.StopReasonError, true},
			{"first-block", adapter.label + " prompt blocked: SAFETY. Safety ratings: ONE, TWO (HIGH)", []string{
				`{"promptFeedback":{"blockReason":"SAFETY","safetyRatings":[{"category":"ONE"},{"probability":"LOW"},{"category":"TWO","probability":"HIGH"}]}}`,
				`{"promptFeedback":{"blockReason":"OTHER"}}`,
			}, goai.StopReasonError, false},
			{"terminal-wins", "", []string{`{"promptFeedback":{"blockReason":"SAFETY"}}`, `{"candidates":[{"finishReason":"STOP"}]}`}, goai.StopReasonStop, false},
			// Preserve existing Go admission of valid candidates with bad optional metadata.
			{"invalid-feedback-keeps-terminal", "", []string{`{"promptFeedback":true,"candidates":[{"finishReason":"STOP"}]}`}, goai.StopReasonStop, false},
		} {
			t.Run(adapter.name+"/"+tc.name, func(t *testing.T) {
				model := googleContinuationModelV160(adapter.api)
				model.BaseURL = "https://upstream.test/v1"
				context := googleContinuationContextV160()
				var body strings.Builder
				for _, chunk := range tc.chunks {
					fmt.Fprintf(&body, "data: %s\n\n", chunk)
				}
				var requests, observed, terminal int
				transport := vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
					defer r.Body.Close()
					requests++
					wantURL := "https://upstream.test" + adapter.path + "test-model:streamGenerateContent?alt=sse"
					if requests != 1 || r.Method != http.MethodPost || r.URL.String() != wantURL ||
						!slices.Equal(r.Header.Values("X-Goog-Api-Key"), []string{"test-key"}) || len(r.Header.Values("Authorization")) != 0 {
						return nil, fmt.Errorf("unexpected scripted Google request %d: %s %s", requests, r.Method, r.URL)
					}
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}, nil
				})
				zero := 0
				opts := &goai.StreamOptions{APIKey: "test-key", MaxRetries: &zero, HTTPTransport: transport,
					OnProviderStreamEvent: func(any, *goai.Model) error { observed++; return nil }}
				var message *goai.Message
				var terminalErr error
				for event := range goai.GetApiProvider(adapter.api).StreamSimple(t.Context(), model, context, opts) {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						terminal++
						message, terminalErr = e.Error, e.Err
					case *goai.DoneEvent:
						terminal++
						message = e.Message
					}
				}
				if requests != 1 || observed != len(tc.chunks) || terminal != 1 || message == nil {
					t.Fatalf("requests=%d chunks=%d terminal=%d message=%+v; want 1/%d/1/result", requests, observed, terminal, message, len(tc.chunks))
				}
				if message.StopReason != tc.stop {
					t.Errorf("stopReason=%q, want %q", message.StopReason, tc.stop)
				}
				if message.ErrorMessage != tc.diagnostic {
					t.Errorf("errorMessage=%q, want %q", message.ErrorMessage, tc.diagnostic)
				}
				if got := goai.IsRetryableAssistantError(message); got != tc.retry {
					t.Errorf("isRetryableAssistantError=%t, want %t", got, tc.retry)
				}
				if (tc.diagnostic == "" && terminalErr != nil) || (tc.diagnostic != "" && (terminalErr == nil || terminalErr.Error() != tc.diagnostic)) {
					t.Errorf("terminal error=%v, want %q", terminalErr, tc.diagnostic)
				}
			})
		}
	}
}
