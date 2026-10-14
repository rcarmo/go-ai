package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestRetry120UpstreamProviderReturnedAnError(t *testing.T) {
	for _, text := range []string{"The upstream provider returned an error", "provider returned error", "provider returned an error"} {
		if !goai.IsRetryableAssistantError(retryMessage(text)) {
			t.Fatal("transient upstream error not retryable", text)
		}
		if goai.IsRetryableAssistantError(retryMessage("quota exceeded: " + text)) {
			t.Fatal("quota precedence lost", text)
		}
	}
	if goai.IsRetryableAssistantError(&goai.Message{StopReason: goai.StopReasonStop, ErrorMessage: "provider returned an error"}) {
		t.Fatal("successful response classified as retryable")
	}
}
