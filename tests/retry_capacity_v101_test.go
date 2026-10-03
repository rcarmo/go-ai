package goai_test

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
	"time"
)

func TestV101CapacityAssistantProductionRetryAndCancellation(t *testing.T) {
	for _, text := range []string{"model is at capacity", "MODEL IS AT CAPACITY", "model is at capacity; billing exhausted"} {
		calls := 0
		result := goai.RetryAssistantCall(context.Background(), func() *goai.Message {
			calls++
			if calls == 1 {
				return &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: text}
			}
			return &goai.Message{StopReason: goai.StopReasonStop}
		}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 1, BaseDelayMs: 1}, nil)
		want := 2
		if text == "model is at capacity; billing exhausted" {
			want = 1
		}
		if calls != want {
			t.Fatalf("%q calls=%d result=%#v", text, calls, result)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	result := goai.RetryAssistantCall(ctx, func() *goai.Message {
		calls++
		return &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "model is at capacity"}
	}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 2, BaseDelayMs: 3600000}, &goai.AssistantRetryCallbacks{OnRetryScheduled: func(_, _ int, delay time.Duration, _ string) {
		if delay != time.Hour {
			t.Errorf("delay=%v", delay)
		}
		cancel()
	}})
	if result.StopReason != goai.StopReasonAborted || calls != 1 {
		t.Fatalf("result=%#v calls=%d", result, calls)
	}
	calls = 0
	result = goai.RetryAssistantCall(context.Background(), func() *goai.Message {
		calls++
		return &goai.Message{StopReason: goai.StopReasonError, ErrorMessage: "model is at capacity"}
	}, &goai.AssistantRetryPolicy{Enabled: true, MaxRetries: 2, BaseDelayMs: 1}, nil)
	if calls != 3 || result.StopReason != goai.StopReasonError {
		t.Fatalf("result=%#v calls=%d", result, calls)
	}
}
