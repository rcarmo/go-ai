package sse

import (
	"strings"
	"testing"
)

func BenchmarkParseSSE140(b *testing.B) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"small", 128}, {"large", 128 << 10}} {
		b.Run(size.name, func(b *testing.B) {
			text := strings.Repeat("x", size.bytes)
			wire := "data: " + text + "\n\ndata: [DONE]\n\n"
			b.ReportAllocs()
			b.SetBytes(int64(len(wire)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				count := 0
				for event := range Parse(strings.NewReader(wire)) {
					if (count == 0 && event.Data != text) || (count == 1 && event.Data != "[DONE]") || event.Event != "message" {
						b.Fatal("event mismatch", count, event.Event)
					}
					count++
				}
				if count != 2 {
					b.Fatal("event count", count)
				}
			}
		})
	}
}
