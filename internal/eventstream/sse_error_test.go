package eventstream_test

import (
	"errors"
	"io"
	"testing"

	"github.com/rcarmo/go-ai/internal/eventstream"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestParseSSESurfacesReaderErrors(t *testing.T) {
	events := eventstream.Parse(io.MultiReader(errReader{}))
	var got []eventstream.SSEEvent
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Event != eventstream.EventError {
		t.Fatalf("expected %q, got %q", eventstream.EventError, got[0].Event)
	}
}
