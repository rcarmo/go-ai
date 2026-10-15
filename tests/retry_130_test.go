package goai_test

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestRetry130TemporaryUnavailableAndForwarding(t *testing.T) {
	for _, text := range []string{"The service is temporarily unavailable. Please retry later.", "Service unavailable: The service is temporarily unavailable.", "unable to forward upstream"} {
		if !goai.IsRetryableAssistantError(&goai.Message{StopReason: goai.StopReasonError, ErrorMessage: text}) {
			t.Fatal(text)
		}
	}
}
