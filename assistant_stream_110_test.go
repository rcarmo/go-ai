package goai

import (
	"testing"
	"time"
)

func TestAssistant110TerminalTimingGuardsAndExplicitZero(t *testing.T) {
	start := time.Now().Add(-12 * time.Millisecond)
	for _, kind := range []string{"done", "error", "old", "existing", "nil"} {
		t.Run(kind, func(t *testing.T) {
			timer := assistantResponseTimer{started: start}
			message := &Message{Role: RoleAssistant, Timestamp: start.UnixMilli()}
			if kind == "old" {
				message.Timestamp--
			}
			if kind == "existing" {
				zero := int64(0)
				message.DurationMs = &zero
			}
			var event Event = &DoneEvent{Message: message}
			if kind == "error" {
				event = &ErrorEvent{Error: message}
			}
			if kind == "nil" {
				event = &DoneEvent{}
			}
			timer.stamp(event)
			switch kind {
			case "old", "nil":
				if message.DurationMs != nil {
					t.Fatal("unobserved response timed")
				}
			case "existing":
				if *message.DurationMs != 0 {
					t.Fatal("explicit duration replaced")
				}
			default:
				if message.DurationMs == nil || *message.DurationMs < 12 {
					t.Fatal("missing monotonic duration", message)
				}
			}
			second := &Message{Timestamp: start.UnixMilli()}
			timer.stamp(&DoneEvent{Message: second})
			if second.DurationMs != nil {
				t.Fatal("second terminal timed")
			}
		})
	}
}
func TestAssistant110ProducerSenderOwnsTimeBeforeDelivery(t *testing.T) {
	events := make(chan Event, 1)
	send := NewAssistantEventSender(events)
	message := &Message{Role: RoleAssistant, Timestamp: time.Now().UnixMilli()}
	progress := make(chan Event, 2)
	progressSend := NewAssistantEventSender(progress)
	progressSend(&StartEvent{Partial: message})
	progressSend(&DoneEvent{Message: message})
	close(progress)
	if first := (<-progress).(*StartEvent); first.Partial.DurationMs != nil {
		t.Fatal("progress acquired later terminal duration")
	}
	message.DurationMs = nil
	send(&DoneEvent{Message: message})
	close(events)
	if message.DurationMs == nil || *message.DurationMs < 0 {
		t.Fatal(message)
	}
	if event := <-events; event.(*DoneEvent).Message != message {
		t.Fatal("terminal ownership changed")
	}
}
