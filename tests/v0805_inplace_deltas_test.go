package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUpstreamOverflowDetectsDS4ConfiguredContextSizeErrors(t *testing.T) {
	msg := &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "400 prompt has 75,288 tokens, but the configured context size is 65,536 tokens"}
	if !goai.IsContextOverflow(msg, 65536) {
		t.Fatal("expected DS4 configured context size error to be detected as overflow")
	}
}

func TestUpstreamRetryMatchesV0805ProviderTransportPatterns(t *testing.T) {
	messages := []string{
		"HTTP 524: timeout from provider edge",
		"The socket connection was closed unexpectedly. For more information, pass `verbose: true` in the second argument to fetch()",
		"ResourceExhausted: gRPC returned resource exhausted from NVIDIA NIM",
	}
	for _, text := range messages {
		msg := &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: text}
		if !goai.IsRetryableAssistantError(msg) {
			t.Fatalf("%q should be retryable", text)
		}
	}
}
