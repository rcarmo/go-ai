package openaicodex

import (
	"context"
	"fmt"
	"github.com/rcarmo/go-ai/transports/websocket"
)

type codexSocketEvent struct {
	data []byte
	err  error
}
type codexSocketLease struct {
	events chan codexSocketEvent
	done   chan struct{}
}

func newCodexSocketLease() *codexSocketLease {
	return &codexSocketLease{events: make(chan codexSocketEvent, 16), done: make(chan struct{})}
}

// Cached sockets must keep reading while idle to process close/control frames.
// Per-request leases isolate queued data across requests. Frames read while
// idle are discarded; cancellation also unblocks a full lease channel.
func startCodexSocketReader(entry *codexWebSocketSessionEntry) {
	ctx, cancel := context.WithCancel(context.Background())
	entry.cancelRead = cancel
	entry.readDone = make(chan struct{})
	go func() {
		defer close(entry.readDone)
		for {
			_, data, err := entry.conn.Read(ctx)
			codexWebSocketSessionsMu.Lock()
			lease := entry.lease
			if err != nil {
				entry.readClosed = true
			}
			codexWebSocketSessionsMu.Unlock()
			if lease != nil {
				select {
				case lease.events <- codexSocketEvent{data, err}:
				case <-lease.done:
				case <-ctx.Done():
				}
			}
			if err != nil {
				return
			}
		}
	}()
}

// Caller holds the registry lock; no user callbacks run on this context.
func stopCodexSocketReader(entry *codexWebSocketSessionEntry) {
	entry.readClosed = true
	if entry.lease != nil {
		close(entry.lease.done)
		entry.lease = nil
	}
	if entry.cancelRead != nil {
		entry.cancelRead()
	}
}

func readCodexSocket(ctx context.Context, conn *websocket.Conn, lease *codexSocketLease) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if lease == nil {
		_, data, err := conn.Read(ctx)
		return data, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lease.done:
		return nil, fmt.Errorf("cached Codex WebSocket closed")
	case event := <-lease.events:
		return event.data, event.err
	}
}
