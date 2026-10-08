package goai

import "testing"

func BenchmarkAssistant110ProgressBoundary(b *testing.B) {
	for _, double := range []bool{true, false} {
		name := "single"
		if double {
			name = "double"
		}
		b.Run(name, func(b *testing.B) {
			ch := make(chan Event, 1)
			send := NewAssistantEventSender(ch)
			event := &TextDeltaEvent{Partial: &Message{Role: RoleAssistant, Content: []ContentBlock{{Type: "text", Text: "partial"}}, Usage: &Usage{Input: 42}}, Delta: "x"}
			b.ReportAllocs()
			for b.Loop() {
				var e Event = event
				if double {
					e = SnapshotEvent(e)
				}
				send(e)
				<-ch
			}
		})
	}
}
