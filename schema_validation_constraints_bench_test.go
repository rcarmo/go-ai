package goai

import "testing"

func BenchmarkV150UniqueScalarArray(b *testing.B) {
	values := make([]interface{}, 10000)
	for i := range values {
		values[i] = i
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if !schemaUniqueItems(values) {
			b.Fatal("unique values rejected")
		}
	}
}
