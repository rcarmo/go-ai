package goai_test

import (
	"context"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

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
