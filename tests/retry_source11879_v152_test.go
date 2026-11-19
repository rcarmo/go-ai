package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

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
