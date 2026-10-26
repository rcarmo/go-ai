package durable

import "testing"

// Nested plain-JSON task and transcript records exercise the dominant bounded
// checkpoint encoder path without a scheduler, filesystem or timing noise.
func BenchmarkJSON140Checkpoint(b *testing.B) {
	entries := make([]any, 32)
	for i := range entries {
		entries[i] = JSON{"kind": "message", "value": JSON{"role": "user", "content": []any{JSON{"type": "text", "text": "checkpoint transcript fixture"}}}}
	}
	value := JSON{"phase": "prepare", "entries": entries, "agent": JSON{"model": JSON{"provider": "fixture", "id": "model"}, "settings": JSON{"enabled": true, "retry": JSON{"maxRetries": 3}}}}
	limits := DefaultLimits()
	data, err := encodeBounded(value, limits, limits.MaxRecordBytes)
	if err != nil {
		b.Fatal(err)
	}
	var decoded JSON
	if err := decodeStrict(data, limits, limits.MaxRecordBytes, &decoded); err != nil || !equalJSONValue(value, decoded) {
		b.Fatal("fixture round trip", err)
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if data, err := encodeBounded(value, limits, limits.MaxRecordBytes); err != nil || len(data) == 0 {
			b.Fatal(err)
		}
	}
}
