package goai_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source retry.test.ts:302 and retry.ts:249: backoff cancellation returns the
// last assistant response with only error removed and stop reason changed.
func TestMainAssistantRetryAbortPreservesResponseMetadata(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	original := &goai.Message{Role: goai.RoleAssistant, Timestamp: 42, Model: "model", ResponseID: "response", Content: []goai.ContentBlock{{Type: "text", Text: "partial"}}, StopReason: goai.StopReasonError, ErrorMessage: "terminated"}
	var finished bool
	var attempts int
	var final string
	calls := 0
	result := goai.RetryAssistantCall(ctx, func() *goai.Message { calls++; return original }, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 2, BaseDelayMs: 1000}, &goai.AssistantRetryCallbacks{
		OnRetryScheduled: func(_, _ int, _ time.Duration, _ string) { cancel() },
		OnRetryFinished: func(success bool, attempt int, message string) {
			finished = success
			attempts = attempt
			final = message
		},
	})
	want := *original
	want.StopReason = goai.StopReasonAborted
	want.ErrorMessage = ""
	if result == original || !reflect.DeepEqual(*result, want) {
		t.Fatalf("aborted response=%+v want=%+v", result, want)
	}
	if original.StopReason != goai.StopReasonError || original.ErrorMessage != "terminated" || calls != 1 || finished || attempts != 1 || final != "terminated" {
		t.Fatalf("original=%+v calls=%d callback=%v/%d/%q", original, calls, finished, attempts, final)
	}
}
