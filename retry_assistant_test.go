package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const openAIExplicitRetryMessage = "An error occurred while processing your request. You can retry your request, or contact us through our help center at help.openai.com if the error persists. Please include the request ID req_******** in your message."
const bedrockExplicitRetryMessage = `{"message":"The system encountered an unexpected error during processing. Try your request again."}`

func retryMessage(errorMessage string) *goai.Message {
	return &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: errorMessage, Usage: &goai.Usage{}}
}

func TestRetryAssistantErrorMatchesExplicitProviderRetryGuidance(t *testing.T) {
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
	if goai.IsRetryableAssistantError(&goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop}) {
		t.Fatal("non-error assistant message should not be retryable")
	}
}
