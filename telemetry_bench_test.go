package goai

import "testing"

func BenchmarkTelemetry140SpanOnly(b *testing.B) {
	model := &Model{ID: "model", Provider: "provider", Api: "api"}
	child := &TelemetrySpan{}
	opts := &StreamOptions{Telemetry: &TelemetrySpan{StartSpan: func(string, map[string]any) *TelemetrySpan { return child }}}
	event := &DoneEvent{Message: &Message{StopReason: StopReasonStop, Usage: &Usage{Input: 1, Output: 1}}}
	b.ReportAllocs()
	for b.Loop() {
		o := observeModelRequest(model, opts, false)
		o.onEvent(event)
		o.end()
	}
}
