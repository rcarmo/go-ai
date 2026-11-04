package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

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
