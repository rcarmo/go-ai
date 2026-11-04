package goai_test

import (
	"context"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Pinned retry.test.ts:169 and #8826: the default agent cap is 60 seconds,
// an explicit cap may be zero, and callbacks see each capped delay.
func TestMainAssistantRetrySourceDelayCap(t *testing.T) {
	for _, tc := range []struct {
		name string
		base int
		cap  *int
		want time.Duration
	}{
		{name: "default", base: 3600000, want: time.Minute},
		{name: "explicit", base: 2000, cap: intPointer(5000), want: 2 * time.Second},
		{name: "zero", base: 2000, cap: intPointer(0), want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var got time.Duration
			calls := 0
			result := goai.RetryAssistantCall(ctx, func() *goai.Message {
				calls++
				return &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "server_busy"}
			}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMs: tc.base, MaxAgentDelayMs: tc.cap}, &goai.AssistantRetryCallbacks{OnRetryScheduled: func(_, _ int, d time.Duration, _ string) { got = d; cancel() }})
			if got != tc.want || calls != 1 || result.StopReason != goai.StopReasonAborted {
				t.Fatalf("delay=%s want=%s calls=%d result=%+v", got, tc.want, calls, result)
			}
		})
	}
	cap := 15
	var delays []time.Duration
	calls := 0
	result := goai.RetryAssistantCall(context.Background(), func() *goai.Message {
		calls++
		if calls == 5 {
			return &goai.Message{StopReason: goai.StopReasonStop}
		}
		return &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "terminated"}
	}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 4, BaseDelayMs: 10, MaxAgentDelayMs: &cap}, &goai.AssistantRetryCallbacks{OnRetryScheduled: func(_, _ int, d time.Duration, _ string) { delays = append(delays, d) }})
	want := []time.Duration{10 * time.Millisecond, 15 * time.Millisecond, 15 * time.Millisecond, 15 * time.Millisecond}
	if len(delays) != len(want) {
		t.Fatalf("delays=%v", delays)
	}
	for i := range want {
		if delays[i] != want[i] {
			t.Errorf("delays=%v want=%v", delays, want)
			break
		}
	}
	if calls != 5 || result.StopReason != goai.StopReasonStop {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
}
func intPointer(value int) *int { return &value }
