package sse_test

import (
	"errors"
	"io"
	"testing"

	"github.com/rcarmo/go-ai/transports/sse"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestParseSSESurfacesReaderErrors(t *testing.T) {
	events := sse.Parse(io.MultiReader(errReader{}))
	var got []sse.SSEEvent
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Event != sse.EventError {
		t.Fatalf("expected %q, got %q", sse.EventError, got[0].Event)
	}
}
