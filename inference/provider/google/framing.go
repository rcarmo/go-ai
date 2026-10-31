package google

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"unicode"

	"github.com/rcarmo/go-ai/transports/sse"
)

const googleIncompleteEOF = "__google_incomplete_eof__"

// parseGoogleFrames mirrors the locked SDK's event boundaries and data-prefix
// rules, not the broader SSE field grammar. Keep the existing per-line scanner
// bound and caller-owned reader/cancellation contract. Mistral uses shared SSE.
func parseGoogleFrames(ctx context.Context, r io.Reader) <-chan sse.SSEEvent {
	ch := make(chan sse.SSEEvent, 16)
	go func() {
		defer close(ch)
		emit := func(event sse.SSEEvent) bool {
			select {
			case <-ctx.Done():
				return false
			case ch <- event:
				return true
			}
		}
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 8*1024), 4*1024*1024)
		scanner.Split(splitGoogleRawLine)
		var buffer []byte
		for ctx.Err() == nil && scanner.Scan() {
			buffer = append(buffer, scanner.Bytes()...)
			for {
				index, length := -1, 0
				for _, delimiter := range []string{"\n\n", "\r\r", "\r\n\r\n"} {
					if at := bytes.Index(buffer, []byte(delimiter)); at >= 0 && (index < 0 || at < index) {
						index, length = at, len(delimiter)
					}
				}
				if index < 0 {
					break
				}
				event := trimGoogleSDKSpace(string(buffer[:index]))
				buffer = buffer[index+length:]
				if strings.HasPrefix(event, "data:") {
					if !emit(sse.SSEEvent{Event: "message", Data: trimGoogleSDKSpace(event[len("data:"):]), Retry: -1}) {
						return
					}
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err := scanner.Err(); err != nil {
			emit(sse.SSEEvent{Event: sse.EventError, Data: err.Error(), Retry: -1})
			return
		}
		if trimGoogleSDKSpace(string(buffer)) != "" {
			emit(sse.SSEEvent{Event: googleIncompleteEOF, Data: "Incomplete JSON segment at the end", Retry: -1})
		}
	}()
	return ch
}

// Preserve CR/LF bytes for the SDK delimiter matcher, including split CRLF.
// Scanner's limit still applies to each physical line, not the whole event.
func splitGoogleRawLine(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		end := i + 1
		if data[i] == '\r' {
			if end == len(data) && !atEOF {
				return 0, nil, nil
			}
			if end < len(data) && data[end] == '\n' {
				end++
			}
		}
		return end, data[:end], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// ECMAScript trim includes BOM but excludes NEL; Go's IsSpace does the reverse.
func trimGoogleSDKSpace(value string) string {
	return strings.TrimFunc(value, func(r rune) bool { return r == '\ufeff' || (r != '\u0085' && unicode.IsSpace(r)) })
}
