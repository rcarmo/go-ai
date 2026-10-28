package sse

import (
	"strings"
	"testing"
)

func TestParseSSE140BufferGrowthAndLimit(t *testing.T) {
	for _, size := range []int{8 << 10, 128 << 10, 4 << 20} {
		text := strings.Repeat("x", size)
		count := 0
		for event := range Parse(strings.NewReader("data: " + text + "\n\n")) {
			count++
			if size == 4<<20 {
				if event.Event != EventError || !strings.Contains(event.Data, "token too long") {
					t.Fatal("line limit bypassed", event.Event, len(event.Data))
				}
			} else if event.Event != "message" || event.Data != text {
				t.Fatal("buffer growth changed data", size, event.Event, len(event.Data))
			}
		}
		if count != 1 {
			t.Fatal("event count", size, count)
		}
	}
}
