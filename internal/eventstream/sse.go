// Package eventstream provides a Server-Sent Events (SSE) line parser.
package eventstream

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string // event type (default: "message")
	Data  string // event data (lines joined by \n)
	ID    string // last event ID
	Retry int    // reconnection time in ms (-1 if not set)
}

// Parse reads SSE events from an io.Reader and sends them to a channel.
// The channel is closed when the reader is exhausted or an error occurs.
func Parse(r io.Reader) <-chan SSEEvent {
	ch := make(chan SSEEvent, 16)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		// SSE can have long lines (base64 images, big JSON)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

		var event SSEEvent
		event.Retry = -1
		var dataLines []string

		for scanner.Scan() {
			line := scanner.Text()

			if line == "" {
				// Empty line = dispatch event
				if len(dataLines) > 0 {
					event.Data = strings.Join(dataLines, "\n")
					if event.Event == "" {
						event.Event = "message"
					}
					ch <- event
				}
				// Reset
				event = SSEEvent{Retry: -1}
				dataLines = nil
				continue
			}

			// Parse field
			if strings.HasPrefix(line, ":") {
				continue // comment
			}

			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")

			switch field {
			case "event":
				event.Event = value
			case "data":
				dataLines = append(dataLines, value)
			case "id":
				event.ID = value
			case "retry":
				// parse int; ignore errors
				var n int
				if _, err := fmt.Sscanf(value, "%d", &n); err == nil {
					event.Retry = n
				}
			}
		}

		// Flush last event if no trailing blank line
		if len(dataLines) > 0 {
			event.Data = strings.Join(dataLines, "\n")
			if event.Event == "" {
				event.Event = "message"
			}
			ch <- event
		}
	}()
	return ch
}
