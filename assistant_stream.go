package goai

import (
	"math"
	"time"
)

// NewAssistantEventSender times terminal responses on the producer goroutine,
// before channel delivery. This also covers directly invoked native providers.
// Use one sender per response, with a single producer. Existing durations and
// messages that started before this response (e.g. deferred fetches) are kept.
func NewAssistantEventSender(events chan<- Event) func(Event) {
	timer := assistantResponseTimer{started: time.Now()}
	return func(event Event) {
		timer.stamp(event)
		// Terminal stamping may mutate a producer Partial; freeze progress before
		// delivery so an earlier event cannot race or acquire terminal metadata.
		events <- SnapshotEvent(event)
	}
}

type assistantResponseTimer struct {
	started  time.Time
	terminal bool
}

func (t *assistantResponseTimer) stamp(event Event) {
	if t.terminal {
		return
	}
	var message *Message
	switch e := event.(type) {
	case *DoneEvent:
		message = e.Message
		t.terminal = true
	case *ErrorEvent:
		message = e.Error
		t.terminal = true
	}
	if message == nil || message.DurationMs != nil || message.Timestamp < t.started.UnixMilli() {
		return
	}
	elapsed := max(int64(0), int64(math.Round(float64(time.Since(t.started))/float64(time.Millisecond))))
	message.DurationMs = &elapsed
}
