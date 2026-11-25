package goai_test

import (
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const responsesEarlyEOFV160 = "OpenAI Responses stream ended before a terminal response event"

// Source: pi v1.6.0 packages/ai/test/retry.test.ts:149–189. Keep the
// provider text exact: the permanent usage codes contain the transient 429.
func TestSourceV160RetryTerminalCapacityAndLimits(t *testing.T) {
	cases := []struct {
		name, message string
		wantRetry     bool
	}{
		{"responses-early-eof", responsesEarlyEOFV160, true},
		{"azure-peak-load", "The system is currently experiencing high demand and cannot process your request. Your request exceeds the maximum usage size allowed during peak load. For improved capacity reliability, consider switching to Provisioned Throughput.", true},
		{"provider-quota", "429 quota exceeded", false},
		{"chatgpt-subscription-limit", `OpenAI API error (429): {"code":"subscription_sharing_usage_limit_exceeded","message":"Usage limit reached."}`, false},
		{"chatgpt-workspace-limit", `OpenAI API error (429): {"code":"subscription_sharing_workplace_individual_usage_limit_exceeded","message":"Usage limit reached."}`, false},
		{"chatgpt-usage-unavailable", "subscription_sharing_usage_unavailable: Usage cannot be checked.", true},
		{"chatgpt-user-unavailable", "subscription_sharing_user_unavailable: User cannot be loaded.", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			failure := retryMessage(tc.message)
			if got := goai.IsRetryableAssistantError(failure); got != tc.wantRetry {
				t.Errorf("IsRetryableAssistantError(%q) = %v; want %v", tc.message, got, tc.wantRetry)
			}

			// Supplemental: the public assistant policy admits the same cases
			// with zero backoff, not just a true/false classifier result.
			var order []string
			calls := 0
			recovered := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, ResponseID: "recovered"}
			result := goai.RetryAssistantCall(t.Context(), func() *goai.Message {
				order = append(order, "produce")
				calls++
				if calls == 1 {
					return failure
				}
				return recovered
			}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 1}, &goai.AssistantRetryCallbacks{
				OnRetryScheduled: func(attempt, maxAttempts int, delay time.Duration, message string) {
					order = append(order, "scheduled")
					if attempt != 1 || maxAttempts != 1 || delay != 0 || message != tc.message {
						t.Errorf("scheduled = (%d, %d, %s, %q); want (1, 1, 0, %q)", attempt, maxAttempts, delay, message, tc.message)
					}
				},
				OnRetryAttemptStart: func() { order = append(order, "retry-start") },
				OnRetryFinished: func(success bool, attempt int, message string) {
					order = append(order, "finished")
					if !success || attempt != 1 || message != "" {
						t.Errorf("finished = (%v, %d, %q); want (true, 1, empty)", success, attempt, message)
					}
				},
			})
			wantOrder, wantResult := []string{"produce"}, failure
			if tc.wantRetry {
				wantOrder, wantResult = []string{"produce", "scheduled", "retry-start", "produce", "finished"}, recovered
			}
			if !slices.Equal(order, wantOrder) || result == nil || result.StopReason != wantResult.StopReason ||
				result.ErrorMessage != wantResult.ErrorMessage || result.ResponseID != wantResult.ResponseID {
				t.Errorf("retry order/result = %v / %+v; want %v / %+v", order, result, wantOrder, wantResult)
			}
		})
	}
}

// Supplemental: the source only classifies a faux message. This exercises
// the actual public Responses stream, then feeds its assistant error into
// the public retry policy. HTTP retries are disabled in both attempts.
func TestV160ResponsesEarlyEOFControlsAssistantRetry(t *testing.T) {
	requests := 0
	transport := providerRetryTransportMain(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		requests++
		if r.Method != http.MethodPost || r.URL.String() != "https://api.openai.com/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
			return nil, fmt.Errorf("unexpected Responses fixture request %s %s or synthetic auth", r.Method, r.URL)
		}
		var body string
		switch requests {
		case 1:
			body = `data: {"type":"response.created","response":{"id":"resp_truncated","status":"in_progress"}}` + "\n\n"
		case 2:
			body = `data: {"type":"response.completed","response":{"id":"resp_recovered","status":"completed","output":[]}}` + "\n\n"
		default:
			return nil, fmt.Errorf("unexpected extra Responses request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	zero := 0
	model := &goai.Model{ID: "gpt-5.1", Api: goai.ApiOpenAIResponses, Provider: goai.ProviderOpenAI, BaseURL: "https://api.openai.com/v1"}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}
	var outcomes []goai.StopReason
	result := goai.RetryAssistantCall(t.Context(), func() *goai.Message {
		msg, err := goai.Complete(t.Context(), model, ctx, &goai.StreamOptions{APIKey: "test-key", HTTPTransport: transport, MaxRetries: &zero})
		if msg == nil {
			t.Fatalf("missing Responses result on request %d: %v", requests, err)
		}
		outcomes = append(outcomes, msg.StopReason)
		if requests == 1 && (err == nil || msg.StopReason != goai.StopReasonError || msg.ErrorMessage != responsesEarlyEOFV160) {
			t.Fatalf("early EOF = %+v, %v; want exact assistant error %q", msg, err, responsesEarlyEOFV160)
		}
		if requests == 2 && err != nil {
			t.Fatalf("recovery request: %v", err)
		}
		return msg
	}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 1}, nil)
	if requests != 2 || !slices.Equal(outcomes, []goai.StopReason{goai.StopReasonError, goai.StopReasonStop}) ||
		result == nil || result.ResponseID != "resp_recovered" || result.ErrorMessage != "" {
		t.Fatalf("requests/outcomes/result = %d / %v / %+v; want one early EOF then recovered stop", requests, outcomes, result)
	}
}
