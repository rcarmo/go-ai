package sse

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type contextReader struct {
	prefix  *strings.Reader
	blocked chan struct{}
	release chan struct{}
	once    sync.Once
	reads   atomic.Int32
	closes  atomic.Int32
}

func (r *contextReader) Read(p []byte) (int, error) {
	r.reads.Add(1)
	if r.prefix.Len() > 0 {
		return r.prefix.Read(p)
	}
	r.once.Do(func() { close(r.blocked) })
	<-r.release
	return 0, io.EOF
}
func (r *contextReader) Close() error { r.closes.Add(1); close(r.release); return nil }
func drainCancelled(t *testing.T, events <-chan SSEEvent) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return
			}
		case <-timer.C:
			t.Fatal("cancelled parser did not finish")
		}
	}
}

func TestParseContextCancelsFullChannel(t *testing.T) {
	reader := &contextReader{prefix: strings.NewReader(strings.Repeat("data: token\n\n", 100)), blocked: make(chan struct{}), release: make(chan struct{})}
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := ParseContext(ctx, reader)
	deadline := time.Now().Add(2 * time.Second)
	for len(events) < cap(events) {
		if time.Now().After(deadline) {
			t.Fatal("parser did not fill buffer")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	drainCancelled(t, events)
	if reader.closes.Load() != 0 {
		t.Fatal("parser closed caller reader")
	}
	select {
	case <-reader.blocked:
		t.Fatal("parser kept reading after cancellation")
	default:
	}
}

func TestParseContextAlreadyCancelledDoesNotRead(t *testing.T) {
	reader := &contextReader{prefix: strings.NewReader("data: token\n\n"), blocked: make(chan struct{}), release: make(chan struct{})}
	defer reader.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	drainCancelled(t, ParseContext(ctx, reader))
	if reader.reads.Load() != 0 || reader.closes.Load() != 0 {
		t.Fatal("pre-cancelled parser accessed caller reader")
	}
}

func TestParseContextBlockedReadRemainsCallerOwned(t *testing.T) {
	reader := &contextReader{prefix: strings.NewReader(""), blocked: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := ParseContext(ctx, reader)
	select {
	case <-reader.blocked:
	case <-time.After(2 * time.Second):
		reader.Close()
		t.Fatal("reader not entered")
	}
	cancel()
	if reader.closes.Load() != 0 {
		t.Fatal("cancellation closed caller reader")
	}
	reader.Close()
	drainCancelled(t, events)
}
