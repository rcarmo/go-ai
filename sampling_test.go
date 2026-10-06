package goai

import "testing"

func TestSamplingThinkingDefaultsClampAndRequestPrecedence(t *testing.T) {
	high := "high"
	model := &Model{Reasoning: true, ThinkingLevelMap: map[ModelThinkingLevel]*string{ModelThinkingLevel(ThinkingHigh): &high, ModelThinkingLevel(ThinkingXHigh): nil}, SamplingParams: map[string]any{"temperature": .8, "top_p": .9}, SamplingParamsByThinkingLevel: map[ModelThinkingLevel]map[string]any{ModelThinkingLevel(ThinkingHigh): {"temperature": .6, "top_k": 32}}}
	got := ResolveSamplingParams(model, ModelThinkingLevel(ThinkingXHigh), map[string]any{"top_p": .4, "temperature": 0})
	if got["temperature"] != 0 || got["top_p"] != .4 || got["top_k"] != 32 {
		t.Fatal(got)
	}
	got["temperature"] = 1
	if model.SamplingParams["temperature"] != .8 || model.SamplingParamsByThinkingLevel[ModelThinkingLevel(ThinkingHigh)]["temperature"] != .6 {
		t.Fatal("sampling inputs mutated")
	}
	if ResolveSamplingParams(nil, ThinkingOff, nil) != nil {
		t.Fatal("empty sampling should omit")
	}
}

func TestHTTP2PendingStreamCancellationIsRetryable(t *testing.T) {
	if !IsRetryableAssistantError(&Message{StopReason: StopReasonError, ErrorMessage: "ERR_HTTP2_STREAM_CANCEL: The pending stream has been canceled (caused by: net::ERR_CONNECTION_TIMED_OUT)"}) {
		t.Fatal("transient HTTP2 cancellation rejected")
	}
	if IsRetryableAssistantError(&Message{StopReason: StopReasonError, ErrorMessage: "insufficient_quota: pending stream has been canceled"}) {
		t.Fatal("quota error made retryable")
	}
}
