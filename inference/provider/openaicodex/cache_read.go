package openaicodex

import (
	"context"
	"fmt"
	"time"

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

// Caller holds the registry lock.
func releaseCodexSocketLease(entry *codexWebSocketSessionEntry) {
	if entry.lease != nil {
		close(entry.lease.done)
		entry.lease = nil
	}
}

// Caller holds the registry lock and detaches this entry from the registry.
// Drop the lease without cancelling the read until the close handshake ends.
func beginCodexGracefulClose(entry *codexWebSocketSessionEntry) bool {
	if entry.gracefulClosing {
		return false
	}
	entry.gracefulClosing = true
	releaseCodexSocketLease(entry)
	if entry.idleTimer != nil {
		entry.idleTimer.Stop()
		entry.idleTimer = nil
	}
	return true
}

func finishCodexGracefulClose(entry *codexWebSocketSessionEntry, reason string) {
	if entry.conn != nil {
		_ = entry.conn.Close(websocket.StatusNormalClosure, reason)
	}
	if entry.cancelRead != nil {
		entry.cancelRead()
	}
}

func stopCodexSocketReader(entry *codexWebSocketSessionEntry) {
	entry.readClosed = true
	releaseCodexSocketLease(entry)
	if entry.cancelRead != nil {
		entry.cancelRead()
	}
}

// Bound each wait for the next frame rather than the lifetime of the request.
// A cached socket's reader is independent of this request's wait on its lease.
func readCodexSocketWithIdleTimeout(ctx context.Context, conn *websocket.Conn, lease *codexSocketLease, timeout time.Duration, timeoutErr error) ([]byte, error) {
	if timeout <= 0 {
		return readCodexSocket(ctx, conn, lease)
	}
	readCtx, cancel := context.WithTimeoutCause(ctx, timeout, timeoutErr)
	defer cancel()
	data, err := readCodexSocket(readCtx, conn, lease)
	if err != nil && ctx.Err() == nil && context.Cause(readCtx) == timeoutErr {
		return nil, timeoutErr
	}
	return data, err
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
