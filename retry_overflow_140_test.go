package goai

import "testing"

// Cases from upstream 1.4.0 test/retry.test.ts (#10920, #10996, #11057)
// plus empty-boundary coverage for the RE2 translation of lookarounds.
func TestRetry140Classification(t *testing.T) {
	for _, text := range []string{
		"request to https://oauth2.googleapis.com/token failed, reason:",
		"request to https://oauth2.googleapis.com/token failed, reason: connect ECONNREFUSED 127.0.0.1:9",
		"unknown: Unable to forward upstream stream", "upstream_error: Upstream request failed",
		"upstream_stream_read_error: Upstream response stream was interrupted", "Upstream HTTP/2 stream failed",
		"The service is temporarily unavailable. Please retry later.",
		"stream ended before a terminal response event", "stream disconnected before completion",
		"subscription_sharing_usage_unavailable: Usage cannot be checked.",
		"subscription_sharing_user_unavailable: User cannot be loaded.",
		"429", "500 error", "520 status code (no body)", "524 status code (no body)",
		"(503)", "_502_", "é504é",
	} {
		t.Run(text, func(t *testing.T) {
			if !IsRetryableAssistantError(&Message{StopReason: StopReasonError, ErrorMessage: text}) {
				t.Fatal("expected transient error")
			}
		})
	}
	for _, text := range []string{
		`401 {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"},"request_id":"req_011CU503xyz"}`,
		"400 image exceeds 5 MB maximum: 5316784 bytes > 5242880 bytes",
		"x429", "429x", "1500", "5030", "",
		"429 quota exceeded", "429 billing error",
		`OpenAI API error (429): {"code":"subscription_sharing_usage_limit_exceeded"}`,
		`OpenAI API error (429): {"code":"subscription_sharing_workplace_individual_usage_limit_exceeded"}`,
	} {
		t.Run(text, func(t *testing.T) {
			if IsRetryableAssistantError(&Message{StopReason: StopReasonError, ErrorMessage: text}) {
				t.Fatal("expected non-retryable error")
			}
		})
	}
	if IsRetryableAssistantError(nil) || IsRetryableAssistantError(&Message{StopReason: StopReasonAborted, ErrorMessage: "503"}) {
		t.Fatal("only assistant error outcomes can retry")
	}
}

func TestOverflow140RequestSizeAndStatusBoundaries(t *testing.T) {
	for _, text := range []string{
		`github-copilot API error (413): {"message":"failed to parse request","code":""}`,
		"413 request rejected", "413: request rejected", "413",
		"Prompt too long", "Range of input length should be [1, 1000]",
		"400 status code (no body)", "413 status code (no body)",
	} {
		if !IsContextOverflow(&Message{StopReason: StopReasonError, ErrorMessage: text}, 0) {
			t.Errorf("expected overflow: %s", text)
		}
	}
	for _, text := range []string{"429 status code (no body)", "error mentions 413 in response body", "4130 rejected", "rate limit: too many tokens"} {
		if IsContextOverflow(&Message{StopReason: StopReasonError, ErrorMessage: text}, 0) {
			t.Errorf("unexpected overflow: %s", text)
		}
	}
}
