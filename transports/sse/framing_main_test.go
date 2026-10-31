package sse

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestSSELineEndingsAndBOM(t *testing.T) {
	for _, boundary := range []string{"\n\n", "\r\r", "\r\n\r\n", "\r\n\r", "\r\n\n", "\n\r\n", "\n\r", "\r\r\n"} {
		t.Run(strings.NewReplacer("\r", "CR", "\n", "LF").Replace(boundary), func(t *testing.T) {
			wire := "\ufeffdata: first" + boundary + "data: second" + boundary
			var events []SSEEvent
			for event := range Parse(iotest.OneByteReader(strings.NewReader(wire))) {
				events = append(events, event)
			}
			if len(events) != 2 || events[0].Data != "first" || events[1].Data != "second" {
				t.Fatalf("events %#v", events)
			}
		})
	}
}

func TestSSESplitCRLFDoesNotDispatchBetweenDataLines(t *testing.T) {
	var events []SSEEvent
	for event := range Parse(iotest.OneByteReader(strings.NewReader("data: first\r\ndata: second\r\n\r\n"))) {
		events = append(events, event)
	}
	if len(events) != 1 || events[0].Data != "first\nsecond" {
		t.Fatalf("events %#v", events)
	}
}

func TestSSEOnlyInitialBOMRemoved(t *testing.T) {
	var events []SSEEvent
	for event := range Parse(iotest.OneByteReader(strings.NewReader("\ufeffdata: first\n\ndata: \ufeffsecond\n\n"))) {
		events = append(events, event)
	}
	if len(events) != 2 || events[0].Data != "first" || events[1].Data != "\ufeffsecond" {
		t.Fatalf("events %#v", events)
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestSSEReadErrorDoesNotFlushIncompleteEvent(t *testing.T) {
	for _, tail := range []string{"data: incomplete\n", "data: incomplete"} {
		var events []SSEEvent
		for event := range Parse(io.MultiReader(strings.NewReader("data: complete\n\n"+tail), failedReader{})) {
			events = append(events, event)
		}
		if len(events) != 2 || events[0].Data != "complete" || events[1].Event != EventError || events[1].Data != io.ErrUnexpectedEOF.Error() {
			t.Fatalf("events %#v", events)
		}
	}
}

func TestSSEEOFInheritsStickyFields(t *testing.T) {
	count := 0
	for event := range Parse(strings.NewReader("id: prior\nretry: 100\n\ndata: last")) {
		count++
		if event.Event != "message" || event.Data != "last" || event.ID != "prior" || event.Retry != 100 {
			t.Fatalf("event %#v", event)
		}
	}
	if count != 1 {
		t.Fatal("event count", count)
	}
}

func TestSSELineSizeLimitPreserved(t *testing.T) {
	count := 0
	for event := range Parse(strings.NewReader("data: " + strings.Repeat("x", 4*1024*1024) + "\n\n")) {
		count++
		if event.Event != EventError {
			t.Fatal("oversized line dispatched", event.Event)
		}
	}
	if count != 1 {
		t.Fatal("event count", count)
	}
}
