package durable

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
	"time"
)

func TestGenerationProgressTrailingCoalescenceStopDropsPendingAndJoinsWrite(t *testing.T) {
	clock := &progressTestClock{time: time.Unix(0, 0), requests: make(chan time.Duration, 8), ticks: make(chan time.Time, 8)}
	writes := make(chan string, 4)
	release := make(chan struct{})
	p := newGenerationProgressClock(func(message MessageReceipt) error { writes <- message.ErrorCode; <-release; return nil }, nil, clock.after)
	t.Cleanup(func() { releaseTaskGate(release); p.stop() })
	p.mark(MessageReceipt{ErrorCode: "first"})
	select {
	case delay := <-clock.requests:
		if delay != 100*time.Millisecond {
			t.Fatal(delay)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("trailing delay missing")
	}
	p.mark(MessageReceipt{ErrorCode: "latest"})
	select {
	case <-writes:
		t.Fatal("partial committed before trailing tick")
	default:
	}
	clock.advance(100 * time.Millisecond)
	select {
	case text := <-writes:
		if text != "latest" {
			t.Fatal(text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("partial write missing")
	}
	p.mark(MessageReceipt{ErrorCode: "must be dropped"})
	stopped := make(chan struct{})
	go func() { p.stop(); close(stopped) }()
	// Wait for stop admission, rather than relying on goroutine launch order.
	awaitTaskSignal(t, p.halt)
	select {
	case <-stopped:
		t.Fatal("stop skipped admitted write join")
	default:
	}
	releaseTaskGate(release)
	awaitTaskSignal(t, stopped)
	select {
	case text := <-writes:
		t.Fatal("pending partial survived stop", text)
	default:
	}
	p.mark(MessageReceipt{ErrorCode: "late"})
	select {
	case text := <-writes:
		t.Fatal("late partial published", text)
	default:
	}
}

func TestGenerationEventPartialTypedNilAndAllPartialBearingEvents(t *testing.T) {
	message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop}
	nilEvents := []goai.Event{(*goai.StartEvent)(nil), (*goai.TextStartEvent)(nil), (*goai.TextDeltaEvent)(nil), (*goai.TextEndEvent)(nil), (*goai.ThinkingStartEvent)(nil), (*goai.ThinkingDeltaEvent)(nil), (*goai.ThinkingEndEvent)(nil), (*goai.ToolCallStartEvent)(nil), (*goai.ToolCallDeltaEvent)(nil), (*goai.ToolCallEndEvent)(nil), nil}
	for _, event := range nilEvents {
		if got := generationEventPartial(event); got != nil {
			t.Fatal("typed-nil partial", got)
		}
	}
	events := []goai.Event{&goai.StartEvent{Partial: message}, &goai.TextStartEvent{Partial: message}, &goai.TextDeltaEvent{Partial: message}, &goai.TextEndEvent{Partial: message}, &goai.ThinkingStartEvent{Partial: message}, &goai.ThinkingDeltaEvent{Partial: message}, &goai.ThinkingEndEvent{Partial: message}, &goai.ToolCallStartEvent{Partial: message}, &goai.ToolCallDeltaEvent{Partial: message}, &goai.ToolCallEndEvent{Partial: message}}
	for _, event := range events {
		if got := generationEventPartial(event); got != message {
			t.Fatalf("%T lost partial", event)
		}
	}
}

func TestGenerationProgressConsumedWakeDoesNotArmEmptyWindow(t *testing.T) {
	timers := make(chan bool, 8)
	ticks := make(chan time.Time, 8)
	writes := make(chan string, 8)
	releaseFirst := make(chan struct{})
	var p *generationProgress
	calls := 0
	p = newGenerationProgressClock(func(message MessageReceipt) error {
		calls++
		writes <- message.ErrorMessage
		if calls == 1 {
			<-releaseFirst
		}
		return nil
	}, nil, func(delay time.Duration) <-chan time.Time {
		p.mu.Lock()
		pending := p.pending != nil
		p.mu.Unlock()
		timers <- pending
		return ticks
	})
	t.Cleanup(func() { releaseTaskGate(releaseFirst); p.stop() })
	p.mark(MessageReceipt{ErrorMessage: "one"})
	if pending := <-timers; !pending {
		t.Fatal("initial empty timer")
	}
	ticks <- time.Now()
	<-writes
	p.mark(MessageReceipt{ErrorMessage: "two"})
	releaseTaskGate(releaseFirst)
	if pending := <-timers; !pending {
		t.Fatal("coalesced empty timer")
	}
	ticks <- time.Now()
	<-writes
	// The stale wake is queued by mark(two) and consumed only after the second
	// write finishes. Observe that consumption, then submit the next real mark.
	deadline := time.After(3 * time.Second)
	for len(p.wake) != 0 {
		select {
		case <-deadline:
			t.Fatal("buffered wake not consumed")
		default:
		}
	}
	p.mark(MessageReceipt{ErrorMessage: "three"})
	select {
	case pending := <-timers:
		if !pending {
			t.Fatal("consumed wake armed an empty timer")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("third window absent")
	}
	ticks <- time.Now()
	select {
	case value := <-writes:
		if value != "three" {
			t.Fatal(value)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("third partial missing")
	}
}
