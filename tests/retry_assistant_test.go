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

// Source: retry.test.ts:191–207 (overload and ordinary stop), with separate
// historical provider wording and stop-class controls.
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
	if goai.IsRetryableAssistantError(&goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop,
		Content: []goai.ContentBlock{{Type: "text", Text: "not an error"}}}) {
		t.Fatal("ordinary stopped assistant message should not be retryable")
	}
	for _, reason := range []goai.StopReason{goai.StopReasonStop, goai.StopReasonAborted} {
		t.Run("stop-class-"+string(reason), func(t *testing.T) {
			message := retryMessage("overloaded_error")
			message.StopReason = reason
			if goai.IsRetryableAssistantError(message) {
				t.Fatalf("%s with transient-looking text is not an assistant error", reason)
			}
		})
	}
}

// Source: retry.test.ts:196–205 and :209–215 (#11057). The last two rows
// are supplemental controls for the same status digits and the status range.
func TestMainRetrySource11057StatusDigitsInsideIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		wantRetry  bool
	}{
		{"standalone-520", "520 status code (no body)", true},
		{"standalone-524", "524 status code (no body)", true},
		{"embedded-503", `401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"},"request_id":"req_011CU503xyz"}`, false},
		{"embedded-524", "400 image exceeds 5 MB maximum: 5316784 bytes > 5242880 bytes", false},
		{"standalone-503", "503 status code (no body)", true},
		{"standalone-525", "525 status code (no body)", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goai.IsRetryableAssistantError(retryMessage(tc.text)); got != tc.wantRetry {
				t.Errorf("retry classification of %q = %v; want %v", tc.text, got, tc.wantRetry)
			}
		})
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

// v1.6.0 packages/ai/test/retry.test.ts: #12124, #12128 and #12064.
func TestSourceV160RetryConnectionFailures(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"Codex error: Responses websocket connection limit reached (60 minutes). Create a new websocket connection to continue.", true},
		{"Codex error: websocket_connection_limit_reached", true},
		{"connection limit reached", false},
		{"read ECONNRESET", true},
		{"connect ETIMEDOUT 140.82.113.3:443", true},
		{"write EPIPE", true},
		{"connect ECONNREFUSED 127.0.0.1:443", true},
		{"ECONNABORTED", true},
		{"Connection reset by peer", true},
		{"Invalid value EXECONNRESET", false},
		{"Unknown field ETIMEDOUTX", false},
		{"Invalid key ERR_EPIPE_ID", false},
		{`{"type":"error","error":{"type":"api_error","message":"Connection closed."}}`, true},
	} {
		if got := goai.IsRetryableAssistantError(retryMessage(tc.text)); got != tc.want {
			t.Errorf("retry(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}
