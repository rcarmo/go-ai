package goai_test

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const openAIExplicitRetryMessage = "An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID req_******** in your message."
const bedrockExplicitRetryMessage = `{"message":"The system encountered an unexpected error during processing. Try your request again."}`

func retryMessage(errorMessage string) *goai.Message {
	return &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: errorMessage, Usage: &goai.Usage{}}
}

func TestRetryAssistantErrorMatchesExplicitProviderRetryGuidance(t *testing.T) {
	for _, message := range []string{"server_busy", "servers are currently busy"} {
		if !goai.IsRetryableAssistantError(retryMessage(message)) {
			t.Fatalf("1.1.0 busy error not retryable: %s", message)
		}
		if goai.IsRetryableAssistantError(retryMessage("quota exceeded: " + message)) {
			t.Fatal("busy text bypassed quota exclusion")
		}
	}
	if !goai.IsRetryableAssistantError(retryMessage("Pending stream has been canceled")) {
		t.Fatal("Bedrock stalled HTTP/2 cancellation must be retryable")
	}
	if goai.IsRetryableAssistantError(retryMessage("quota exceeded: pending stream has been canceled")) {
		t.Fatal("quota exclusion must precede retry phrase")
	}
	if !goai.IsRetryableAssistantError(retryMessage(openAIExplicitRetryMessage)) {
		t.Fatal("expected OpenAI explicit retry guidance to be retryable")
	}
	if !goai.IsRetryableAssistantError(retryMessage(bedrockExplicitRetryMessage)) {
		t.Fatal("expected Bedrock explicit retry guidance to be retryable")
	}
}

func TestRetryAssistantErrorKeepsProviderLimitErrorsNonRetryable(t *testing.T) {
	if goai.IsRetryableAssistantError(retryMessage("429 quota exceeded")) {
		t.Fatal("quota exceeded should be non-retryable")
	}
}

func TestRetryAssistantErrorClassifiesAssistantErrorMessages(t *testing.T) {
	if !goai.IsRetryableAssistantError(retryMessage("overloaded_error")) {
		t.Fatal("overloaded_error should be retryable")
	}
	if !goai.IsRetryableAssistantError(retryMessage("Error: exceeded request buffer limit while retrying upstream")) {
		t.Fatal("upstream request-buffer exhaustion should be retryable")
	}
	if !goai.IsRetryableAssistantError(retryMessage("currently experiencing high demand")) {
		t.Fatal("v0.87 high-demand provider error should be retryable")
	}
	if !goai.IsRetryableAssistantError(retryMessage("HTTP 520 from provider edge")) {
		t.Fatal("v0.87 HTTP 520 provider error should be retryable")
	}
	if goai.IsRetryableAssistantError(&goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop}) {
		t.Fatal("non-error assistant message should not be retryable")
	}
}

// Pinned packages/ai/test/retry.test.ts:159 (#11057): status-like digits
// inside an identifier or longer number are not retryable status codes.
func TestMainRetrySource11057StatusDigitsInsideIdentifiers(t *testing.T) {
	for _, text := range []string{
		`401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"},"request_id":"req_011CU503xyz"}`,
		"400 image exceeds 5 MB maximum: 5316784 bytes > 5242880 bytes",
	} {
		if goai.IsRetryableAssistantError(&goai.Message{StopReason: goai.StopReasonError, ErrorMessage: text}) {
			t.Errorf("embedded status retried: %q", text)
		}
	}
	for _, text := range []string{"520 status code (no body)", "524 status code (no body)"} {
		if !goai.IsRetryableAssistantError(&goai.Message{StopReason: goai.StopReasonError, ErrorMessage: text}) {
			t.Errorf("standalone status not retried: %q", text)
		}
	}
}

// Source-derived from pinned packages/ai/src/utils/retry.ts: Zhipu/Z.ai
// code 1308 takes precedence over HTTP 429 and textual retry guidance. No
// upstream test for this rule was found at the pin.
func TestMainRetryZhipu1308ProviderLimitPrecedesTransientGuidance(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{`HTTP 429 {"code":1308,"message":"rate limit; retry your request"}`, false},
		{`HTTP 429 {"code" : "1308","message":"server busy"}`, false},
		{`HTTP 429 {"CODE":"1308","message":"try your request again"}`, false},
		{`HTTP 429 {"code":13080,"message":"server busy"}`, true},
		{`HTTP 429 {"code":"13081","message":"server busy"}`, true},
		{`HTTP 429 {"error_code":1308,"message":"server busy"}`, true},
		{`HTTP 429 code 1308 server busy`, true},
		{`{"code":1308,"message":"usage limit"}`, false},
	} {
		message := &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: tc.text}
		if got := goai.IsRetryableAssistantError(message); got != tc.want {
			t.Errorf("%q: got %v want %v", tc.text, got, tc.want)
		}
	}
	if goai.IsRetryableAssistantError(&goai.Message{StopReason: goai.StopReasonStop, ErrorMessage: `HTTP 429 {"code":13080}`}) {
		t.Fatal("non-error stop retried")
	}
}

// A source-derived downstream control checks real retry admission, not only
// the pattern helper. The neighbouring longer code remains retryable.
func TestMainRetryZhipu1308ControlsRetryAdmission(t *testing.T) {
	for _, tc := range []struct {
		text      string
		wantCalls int
	}{
		{`HTTP 429 {"code":"1308","message":"server busy"}`, 1},
		{`HTTP 429 {"code":"13080","message":"server busy"}`, 2},
	} {
		calls := 0
		result := goai.RetryAssistantCall(context.Background(), func() *goai.Message {
			calls++
			if calls == 1 {
				return &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: tc.text}
			}
			return &goai.Message{StopReason: goai.StopReasonStop}
		}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMs: 1}, nil)
		if calls != tc.wantCalls {
			t.Errorf("%q: calls=%d want=%d result=%+v", tc.text, calls, tc.wantCalls, result)
		}
	}
}

// Pinned packages/ai/test/retry.test.ts:100 (#11879).
func TestSourceV152RetryGatewayStreamCutShort(t *testing.T) {
	const text = "Claude Code's stream from Anthropic was cut short after the reply had begun"
	if !goai.IsRetryableAssistantError(retryMessage(text)) {
		t.Fatalf("gateway stream interruption was not retryable: %q", text)
	}
}

func TestV152RetryGatewayStreamCutShortControls(t *testing.T) {
	for _, text := range []string{
		"Claude Code's stream from Anthropic was cut short before the reply had begun",
		"quota exceeded: Claude Code's stream from Anthropic was cut short after the reply had begun",
	} {
		if goai.IsRetryableAssistantError(retryMessage(text)) {
			t.Errorf("unexpected retry: %q", text)
		}
	}
}
