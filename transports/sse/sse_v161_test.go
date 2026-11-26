package sse_test

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/transports/sse"
)

// sseReadChunks splits bytes, including the bytes within a UTF-8 rune.
type sseReadChunks struct {
	*strings.Reader
	chunkSize int
}

func (r sseReadChunks) Read(p []byte) (int, error) {
	return r.Reader.Read(p[:min(len(p), r.chunkSize)])
}

func sseEvents(stream <-chan sse.SSEEvent) (events []sse.SSEEvent) {
	for event := range stream {
		events = append(events, event)
	}
	return events
}

func TestSourceV161SSEEveryChunkSize(t *testing.T) {
	want := []sse.SSEEvent{
		{Event: "delta", Data: "{\"a\":\n \"é\"}", Retry: -1, Raw: []string{"event: delta", `data: {"a":`, `data:  "é"}`}},
		{Event: "message", Data: "tail", Retry: -1, Raw: []string{"data: tail"}},
	}
	for _, separator := range []string{"\n", "\r\n", "\r"} {
		wire := strings.Join([]string{": keepalive", "event: delta", `data: {"a":`, `data:  "é"}`, "", "data: tail"}, separator)
		for size := 1; size <= len(wire); size++ {
			got := sseEvents(sse.Parse(sseReadChunks{strings.NewReader(wire), size}))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("separator %q, chunk size %d: got %#v, want %#v", separator, size, got, want)
			}
		}
	}
}

// sseAnnouncedReader lets the test check the buffered event channel after the
// parser consumed the first bytes but before it can see the next bytes or EOF.
type sseAnnouncedReader struct {
	r       io.Reader
	started chan struct{}
}

func (r *sseAnnouncedReader) Read(p []byte) (int, error) {
	if r.started != nil {
		close(r.started)
		r.started = nil
	}
	return r.r.Read(p)
}

func TestSourceV161SSESplitCRLF(t *testing.T) {
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	started := make(chan struct{})
	stream := sse.Parse(io.MultiReader(strings.NewReader("data: a\r"), &sseAnnouncedReader{r, started}))
	<-started
	select {
	case early := <-stream:
		t.Fatalf("dispatched after first chunk: %#v", early)
	default:
	}
	if _, err := io.WriteString(w, "\ndata: b\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	want := []sse.SSEEvent{{Event: "message", Data: "a\nb", Retry: -1, Raw: []string{"data: a", "data: b"}}}
	if got := sseEvents(stream); !reflect.DeepEqual(got, want) {
		t.Fatalf("split CRLF: got %#v, want %#v", got, want)
	}
}

func TestSourceV161SSEDiscardEmptyFrame(t *testing.T) {
	want := []sse.SSEEvent{{Event: "message", Data: "x", Retry: -1, Raw: []string{"data: x"}}}
	t.Run("empty-event-name", func(t *testing.T) {
		if got := sseEvents(sse.Parse(strings.NewReader("event:\n\ndata: x\n\n"))); !reflect.DeepEqual(got, want) {
			t.Fatalf("empty event name: got %#v, want %#v", got, want)
		}
	})
	t.Run("comment-only", func(t *testing.T) {
		if got := sseEvents(sse.Parse(strings.NewReader(": ping\n\ndata: x\n\n"))); !reflect.DeepEqual(got, want) {
			t.Fatalf("comment-only frame: got %#v, want %#v", got, want)
		}
	})
}
