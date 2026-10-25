package goai

import "testing"

func BenchmarkRegisterBuiltinModels(b *testing.B) {
	RegisterBuiltinModels() // Compare equivalent, already-populated runtimes.
	b.ReportAllocs()
	for b.Loop() {
		RegisterBuiltinModels()
	}
}
