package goai

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestUUIDv7UsesRFC9562LayoutAndPreservesMonotonicOrder(t *testing.T) {
	uuidV7State.Lock()
	oldNow, oldRandom := uuidV7State.now, uuidV7State.random
	uuidV7State.lastTimestamp = -1
	uuidV7State.sequence = 0
	randoms := [][]byte{
		{0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xfe, 0x01, 0x11, 0x22, 0x33, 0x44, 0x55},
		make([]byte, 16),
		make([]byte, 16),
	}
	idx := 0
	uuidV7State.now = func() int64 { return 0x0123456789ab }
	uuidV7State.random = func(dst []byte) error {
		copy(dst, randoms[idx])
		idx++
		return nil
	}
	uuidV7State.Unlock()
	t.Cleanup(func() {
		uuidV7State.Lock()
		uuidV7State.now, uuidV7State.random = oldNow, oldRandom
		uuidV7State.lastTimestamp = -1
		uuidV7State.sequence = 0
		uuidV7State.Unlock()
	})

	first := UUIDv7()
	second := UUIDv7()
	third := UUIDv7()
	if first != "01234567-89ab-7fff-bfff-f91122334455" || second != "01234567-89ab-7fff-bfff-fc0000000000" || third != "01234567-89ac-7000-8000-000000000000" {
		t.Fatalf("unexpected UUIDs: %s %s %s", first, second, third)
	}
	if !(first < second && second < third) {
		t.Fatalf("UUIDs not monotonic: %s %s %s", first, second, third)
	}
}

func TestContentTextExtractsTextBlocks(t *testing.T) {
	blocks := []ContentBlock{{Type: "thinking", Thinking: "reasoning"}, {Type: "text", Text: "first"}, {Type: "toolCall", ID: "1", Name: "read"}, {Type: "text", Text: "second"}}
	if got := ContentText(blocks); got != "first\nsecond" {
		t.Fatalf("ContentText default = %q", got)
	}
	if got := ContentText(blocks, ""); got != "firstsecond" {
		t.Fatalf("ContentText empty separator = %q", got)
	}
	if got := ContentText("hello"); got != "hello" {
		t.Fatalf("ContentText string = %q", got)
	}
}

func TestRetryAssistantCallReportsAbortedRetriedCallUnsuccessful(t *testing.T) {
	attempts := 0
	var finished []any
	res := RetryAssistantCall(context.Background(), func() *Message {
		attempts++
		if attempts == 1 {
			return &Message{Role: RoleAssistant, StopReason: StopReasonError, ErrorMessage: "terminated"}
		}
		return &Message{Role: RoleAssistant, StopReason: StopReasonAborted}
	}, &AssistantRetryPolicy{Enabled: true, MaxRetries: 3}, &AssistantRetryCallbacks{OnRetryFinished: func(success bool, attempt int, finalError string) {
		finished = []any{success, attempt, finalError}
	}})
	if res.StopReason != StopReasonAborted || attempts != 2 {
		t.Fatalf("unexpected result attempts=%d res=%#v", attempts, res)
	}
	if !reflect.DeepEqual(finished, []any{false, 1, ""}) {
		t.Fatalf("finished=%#v", finished)
	}
}

func TestRetryAssistantCallAbortedBackoffReturnsAbortedAndUnsuccessful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	finished := false
	resCh := make(chan *Message, 1)
	go func() {
		resCh <- RetryAssistantCall(ctx, func() *Message {
			return &Message{Role: RoleAssistant, StopReason: StopReasonError, ErrorMessage: "terminated"}
		}, &AssistantRetryPolicy{Enabled: true, MaxRetries: 5, BaseDelayMs: 10_000}, &AssistantRetryCallbacks{OnRetryScheduled: func(int, int, time.Duration, string) {
			cancel()
		}, OnRetryFinished: func(success bool, attempt int, finalError string) {
			finished = !success && attempt == 1 && finalError == "terminated"
		}})
	}()
	select {
	case res := <-resCh:
		if res.StopReason != StopReasonAborted || !finished {
			t.Fatalf("unexpected aborted retry res=%#v finished=%v", res, finished)
		}
	case <-time.After(time.Second):
		t.Fatal("retry did not abort")
	}
}
