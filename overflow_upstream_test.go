package goai_test

import (
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func createOverflowErrorMessage(errorMessage string) *goai.Message {
	return &goai.Message{Role: goai.RoleAssistant, Content: []goai.ContentBlock{}, Api: goai.ApiOpenAICompletions, Provider: "ollama", Model: "qwen3.5:35b", Usage: &goai.Usage{}, StopReason: goai.StopReasonError, ErrorMessage: errorMessage}
}

func TestOverflowDetectsExplicitOllamaPromptTooLongErrors(t *testing.T) {
	msg := createOverflowErrorMessage("400 `prompt too long; exceeded max context length by 100918 tokens`")
	if !goai.IsContextOverflow(msg, 32768) {
		t.Fatal("expected overflow")
	}
}

func TestOverflowDetectsTogetherAIContextLengthErrors(t *testing.T) {
	msg := createOverflowErrorMessage("400 The input (516368 tokens) is longer than the model's context length (262144 tokens).")
	if !goai.IsContextOverflow(msg, 262144) {
		t.Fatal("expected overflow")
	}
}

func TestOverflowDetectsLiteLLMWrappedOpenAIMaximumContextLengthErrors(t *testing.T) {
	msg := createOverflowErrorMessage("Error: 503 litellm.ServiceUnavailableError: litellm.MidStreamFallbackError: litellm.APIConnectionError: APIConnectionError: OpenAIException - Requested token count exceeds the model's maximum context length of 131072 tokens.")
	if !goai.IsContextOverflow(msg, 131072) {
		t.Fatal("expected overflow")
	}
}

func TestOverflowDetectsOpenAICompatibleParenthesizedMaximumContextLengthErrors(t *testing.T) {
	msg := createOverflowErrorMessage("Error: 400 Input length (265330) exceeds model's maximum context length (262144).")
	if !goai.IsContextOverflow(msg, 262144) {
		t.Fatal("expected overflow")
	}
}

func TestOverflowDetectsOpenRouterPoolsideMaximumAllowedInputLengthErrors(t *testing.T) {
	msg := createOverflowErrorMessage("Provider returned error: Input length 131393 exceeds the maximum allowed input length of 131040 tokens.")
	if !goai.IsContextOverflow(msg, 131072) {
		t.Fatal("expected overflow")
	}
}

func TestOverflowDoesNotTreatGenericNonOverflowOllamaErrorsAsOverflow(t *testing.T) {
	msg := createOverflowErrorMessage("500 `model runner crashed unexpectedly`")
	if goai.IsContextOverflow(msg, 32768) {
		t.Fatal("did not expect overflow")
	}
}

func TestOverflowDoesNotTreatBedrockThrottlingTooManyTokensAsOverflow(t *testing.T) {
	msg := createOverflowErrorMessage("Throttling error: Too many tokens, please wait before trying again.")
	if goai.IsContextOverflow(msg, 200000) {
		t.Fatal("did not expect overflow")
	}
}

func TestOverflowDoesNotTreatBedrockServiceUnavailableAsOverflow(t *testing.T) {
	msg := createOverflowErrorMessage("Service unavailable: The service is temporarily unavailable.")
	if goai.IsContextOverflow(msg, 200000) {
		t.Fatal("did not expect overflow")
	}
}

func TestOverflowDoesNotTreatGenericRateLimitErrorsAsOverflow(t *testing.T) {
	msg := createOverflowErrorMessage("Rate limit exceeded, please retry after 30 seconds.")
	if goai.IsContextOverflow(msg, 200000) {
		t.Fatal("did not expect overflow")
	}
}

func TestOverflowDoesNotTreatHTTP429StyleErrorsAsOverflow(t *testing.T) {
	msg := createOverflowErrorMessage("Too many requests. Please slow down.")
	if goai.IsContextOverflow(msg, 200000) {
		t.Fatal("did not expect overflow")
	}
}

func createOverflowLengthStopMessage(input, cacheRead, output int) *goai.Message {
	return &goai.Message{Role: goai.RoleAssistant, Api: goai.ApiOpenAICompletions, Provider: goai.ProviderXiaomi, Model: "mimo-v2.5-pro", Usage: &goai.Usage{Input: input, CacheRead: cacheRead, Output: output, TotalTokens: input + cacheRead + output}, StopReason: goai.StopReasonLength}
}

func TestOverflowDetectsXiaomiStyleOverflow(t *testing.T) {
	msg := createOverflowLengthStopMessage(58, 1048512, 0)
	if !goai.IsContextOverflow(msg, 1048576) {
		t.Fatal("expected overflow")
	}
}

func TestOverflowDoesNotTreatNormalLengthStopsWithOutputAsOverflow(t *testing.T) {
	msg := createOverflowLengthStopMessage(1000, 0, 4096)
	if goai.IsContextOverflow(msg, 200000) {
		t.Fatal("did not expect overflow")
	}
}

func TestOverflowDoesNotTreatLengthStopsFarBelowContextAsOverflow(t *testing.T) {
	msg := createOverflowLengthStopMessage(100, 0, 0)
	if goai.IsContextOverflow(msg, 200000) {
		t.Fatal("did not expect overflow")
	}
}
