package sse_test

import (
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/transports/sse"
)

func TestParseSSE(t *testing.T) {
	input := `event: message_start
data: {"type":"message_start"}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"text":"Hello"}}

data: [DONE]

`
	events := sse.Parse(strings.NewReader(input))
	var got []sse.SSEEvent
	for e := range events {
		got = append(got, e)
	}

	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].Event != "message_start" {
		t.Fatalf("expected event 'message_start', got %q", got[0].Event)
	}
	if got[1].Event != "content_block_delta" {
		t.Fatalf("expected event 'content_block_delta', got %q", got[1].Event)
	}
	if got[2].Data != "[DONE]" {
		t.Fatalf("expected data '[DONE]', got %q", got[2].Data)
	}
}

func TestParseMultilineData(t *testing.T) {
	input := "data: line1\ndata: line2\n\n"
	events := sse.Parse(strings.NewReader(input))
	var got []sse.SSEEvent
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Data != "line1\nline2" {
		t.Fatalf("expected multiline data, got %q", got[0].Data)
	}
}

func TestParseStickyIDAndRetry(t *testing.T) {
	// SSE spec: id and retry persist across events until overwritten
	input := "id: 1\nretry: 5000\ndata: first\n\ndata: second\n\nid: 3\ndata: third\n\n"
	events := sse.Parse(strings.NewReader(input))
	var got []sse.SSEEvent
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	// First event: explicit id and retry
	if got[0].ID != "1" || got[0].Retry != 5000 {
		t.Fatalf("event 0: expected id=1 retry=5000, got id=%q retry=%d", got[0].ID, got[0].Retry)
	}
	// Second event: sticky id and retry from first
	if got[1].ID != "1" || got[1].Retry != 5000 {
		t.Fatalf("event 1: expected sticky id=1 retry=5000, got id=%q retry=%d", got[1].ID, got[1].Retry)
	}
	// Third event: overwritten id, sticky retry
	if got[2].ID != "3" || got[2].Retry != 5000 {
		t.Fatalf("event 2: expected id=3 retry=5000, got id=%q retry=%d", got[2].ID, got[2].Retry)
	}
}
