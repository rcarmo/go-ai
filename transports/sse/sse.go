// Package sse provides a Server-Sent Events (SSE) line parser.
package sse

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string   // event type (default: "message")
	Data  string   // event data (lines joined by \n)
	ID    string   // last event ID
	Retry int      // reconnection time in ms (-1 if not set)
	Raw   []string // raw SSE lines for this event, excluding comments
}

const EventError = "__error__"

// Parse reads SSE events from an io.Reader and sends them to a channel.
// The channel is closed when the reader is exhausted or an error occurs.
func Parse(r io.Reader) <-chan SSEEvent { return ParseContext(context.Background(), r) }

// ParseContext makes parsing and channel sends cancellation-aware. Events
// already buffered may still be received after cancellation. It never closes r
// or interrupts a blocked Read: callers must close/unblock their reader. HTTP
// callers should use their request context and close the body on early exit.
func ParseContext(ctx context.Context, r io.Reader) <-chan SSEEvent {
	ch := make(chan SSEEvent, 16)
	go func() {
		defer close(ch)
		emit := func(event SSEEvent) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- event:
				return true
			}
		}
		scanner := bufio.NewScanner(r)
		// Start small for ordinary token events; grow to the same hard limit
		// for long lines such as base64 images and large JSON records.
		scanner.Buffer(make([]byte, 0, 8*1024), 4*1024*1024)

		var event SSEEvent
		event.Retry = -1
		var dataLines []string
		var rawLines []string
		// SSE spec: id and retry are sticky across events until overwritten.
		lastID := ""
		lastRetry := -1

		for ctx.Err() == nil && scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				// Empty line = dispatch event
				if len(dataLines) > 0 {
					event.Data = strings.Join(dataLines, "\n")
					event.Raw = append([]string(nil), rawLines...)
					if event.Event == "" {
						event.Event = "message"
					}
					if event.ID == "" {
						event.ID = lastID
					}
					if event.Retry == -1 {
						event.Retry = lastRetry
					}
					if !emit(event) {
						return
					}
				}
				// Reset per-event state; keep sticky id/retry
				event = SSEEvent{Retry: -1}
				dataLines = nil
				rawLines = nil
				continue
			}

			// Parse field
			if strings.HasPrefix(line, ":") {
				continue // comment
			}

			rawLines = append(rawLines, line)
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")

			switch field {
			case "event":
				event.Event = value
			case "data":
				dataLines = append(dataLines, value)
			case "id":
				event.ID = value
				lastID = value
			case "retry":
				// parse int; ignore errors
				var n int
				if _, err := fmt.Sscanf(value, "%d", &n); err == nil {
					event.Retry = n
					lastRetry = n
				}
			}
		}

		if ctx.Err() != nil {
			return
		}

		// Flush last event if no trailing blank line
		if len(dataLines) > 0 {
			event.Data = strings.Join(dataLines, "\n")
			event.Raw = append([]string(nil), rawLines...)
			if event.Event == "" {
				event.Event = "message"
			}
			if !emit(event) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			emit(SSEEvent{Event: EventError, Data: err.Error(), Retry: -1})
		}
	}()
	return ch
}
