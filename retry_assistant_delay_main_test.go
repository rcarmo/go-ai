package goai

import (
	"testing"
	"time"
)

// Pinned retry.test.ts:169–175, plus overflow and duration-boundary controls.
func TestMainAssistantRetryDelaySourceCapsAndOverflow(t *testing.T) {
	fiveThousand, zero := 5000, 0
	for _, tc := range []struct {
		name    string
		policy  AssistantRetryPolicy
		attempt int
		want    time.Duration
	}{
		{"source-default", AssistantRetryPolicy{BaseDelayMs: 2000}, 6, time.Minute},
		{"source-explicit", AssistantRetryPolicy{BaseDelayMs: 2000, MaxAgentDelayMs: &fiveThousand}, 5, 5 * time.Second},
		{"source-zero", AssistantRetryPolicy{BaseDelayMs: 2000, MaxAgentDelayMs: &zero}, 5, 0},
		{"large-attempt", AssistantRetryPolicy{BaseDelayMs: 1}, 10000, time.Minute},
		{"large-base", AssistantRetryPolicy{BaseDelayMs: int(^uint(0) >> 1)}, 10000, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := assistantRetryDelay(&tc.policy, tc.attempt); got != tc.want {
				t.Fatalf("delay=%s want=%s", got, tc.want)
			}
		})
	}
	max := int(^uint(0) >> 1)
	got := assistantRetryDelay(&AssistantRetryPolicy{BaseDelayMs: 1, MaxAgentDelayMs: &max}, 10000)
	want := time.Duration(int64((1<<63-1)/int64(time.Millisecond))) * time.Millisecond
	// On 32-bit systems the supplied cap is smaller than the duration bound.
	if int64(max) < int64((1<<63-1)/int64(time.Millisecond)) {
		want = time.Duration(max) * time.Millisecond
	}
	if got != want {
		t.Fatalf("bounded duration=%s want=%s", got, want)
	}
}
