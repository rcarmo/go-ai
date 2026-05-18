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
