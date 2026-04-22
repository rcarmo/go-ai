package eventstream_test

import (
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/internal/eventstream"
)

func FuzzSSEParse(f *testing.F) {
	// Seed corpus: valid SSE streams and edge cases
	f.Add("data: hello\n\n")
	f.Add("event: message\ndata: {\"type\":\"text\"}\n\n")
	f.Add("data: line1\ndata: line2\n\n")
	f.Add("data: [DONE]\n\n")
	f.Add(": comment\ndata: after comment\n\n")
	f.Add("event: content_block_start\ndata: {\"index\":0}\n\nevent: content_block_delta\ndata: {\"delta\":\"hi\"}\n\n")
	f.Add("id: 123\ndata: with id\n\n")
	f.Add("retry: 5000\ndata: retry\n\n")
	f.Add("")
	f.Add("\n\n\n")
	f.Add("data: \n\n") // empty data line
	f.Add("event: \ndata: empty event\n\n")
	f.Add("data: {\"key\":\"value with\\nnewline\"}\n\n")

	f.Fuzz(func(t *testing.T, input string) {
		// Must not panic
		events := eventstream.Parse(strings.NewReader(input))
		count := 0
		for e := range events {
			count++
			// Basic sanity
			_ = e.Event
			_ = e.Data
			_ = e.ID
			if count > 10000 {
				// Prevent infinite loops from adversarial input
				break
			}
		}
	})
}
