package goai_test

import (
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source: pi v1.6.0 retry.test.ts:231–269. The distinct-final-error row
// separately checks that exhaustion reports the last error rather than a prior one.
func TestSourceV160RetryAssistantTerminalResults(t *testing.T) {
	type scheduledRetry struct {
		attempt, limit int
		delay          time.Duration
		message        string
	}
	type finishedRetry struct {
		success bool
		attempt int
		message string
	}
	for _, tc := range []struct {
		name, content string
		reason        goai.StopReason
		attemptErrors []string
	}{
		{"success", "ok", goai.StopReasonStop, []string{""}},
		{"aborted", "", goai.StopReasonAborted, []string{""}},
		{"permanent-quota", "", goai.StopReasonError, []string{"insufficient_quota"}},
		{"transient-exhaustion", "", goai.StopReasonError, []string{"terminated", "terminated", "terminated", "terminated"}},
		{"distinct-final-error", "", goai.StopReasonError, []string{"terminated", "server_busy", "overloaded_error", "ECONNRESET"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fixture := func(message string) *goai.Message {
					return &goai.Message{Role: goai.RoleAssistant, StopReason: tc.reason, ErrorMessage: message,
						Content: []goai.ContentBlock{{Type: "text", Text: tc.content}}}
				}
				const maxRetries = 3
				var scheduled []scheduledRetry
				var finished []finishedRetry
				var events []string
				calls := 0
				start := time.Now()
				result := goai.RetryAssistantCall(t.Context(), func() *goai.Message {
					events = append(events, "produce")
					calls++
					if calls > len(tc.attemptErrors) {
						return &goai.Message{StopReason: goai.StopReasonStop, ErrorMessage: "unexpected extra produce call"}
					}
					return fixture(tc.attemptErrors[calls-1])
				}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: maxRetries, BaseDelayMs: 0}, &goai.AssistantRetryCallbacks{
					OnRetryScheduled: func(attempt, limit int, delay time.Duration, message string) {
						events = append(events, "scheduled")
						scheduled = append(scheduled, scheduledRetry{attempt, limit, delay, message})
					},
					OnRetryAttemptStart: func() { events = append(events, "retry-start") },
					OnRetryFinished: func(success bool, attempt int, message string) {
						events = append(events, "finished")
						finished = append(finished, finishedRetry{success, attempt, message})
					},
				})
				if elapsed := time.Since(start); elapsed != 0 {
					t.Errorf("virtual elapsed time = %s, want no delay", elapsed)
				}
				wantCalls := len(tc.attemptErrors)
				lastError := tc.attemptErrors[wantCalls-1]
				if calls != wantCalls {
					t.Errorf("produce calls = %d, want %d", calls, wantCalls)
				}
				if want := fixture(lastError); !reflect.DeepEqual(result, want) {
					t.Errorf("returned assistant message = %+v; want %+v", result, want)
				}
				var wantScheduled []scheduledRetry
				var wantFinished []finishedRetry
				wantEvents := []string{"produce"}
				for i, message := range tc.attemptErrors[:wantCalls-1] {
					wantScheduled = append(wantScheduled, scheduledRetry{i + 1, maxRetries, 0, message})
					wantEvents = append(wantEvents, "scheduled", "retry-start", "produce")
				}
				if wantCalls > 1 {
					wantFinished = []finishedRetry{{false, maxRetries, lastError}}
					wantEvents = append(wantEvents, "finished")
				}
				if !slices.Equal(scheduled, wantScheduled) {
					t.Errorf("scheduled callbacks = %+v; want %+v", scheduled, wantScheduled)
				}
				if !slices.Equal(finished, wantFinished) {
					t.Errorf("finished callbacks = %+v; want %+v", finished, wantFinished)
				}
				if !slices.Equal(events, wantEvents) {
					t.Errorf("retry events = %v; want %v", events, wantEvents)
				}
			})
		})
	}
}
