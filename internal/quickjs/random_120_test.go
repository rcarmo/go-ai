package quickjs

import "testing"

func TestQuickJS120MulberryWASIReference(t *testing.T) {
	r := quickjsRandom{state: 42 ^ 0x9e3779b9}
	got := make([]byte, 8)
	r.Read(got)
	want := []byte{123, 252, 188, 156, 180, 247, 142, 170}
	for i := range got {
		if got[i] != want[i] {
			t.Fatal("WASI PRNG mismatch", got, want)
		}
	}
	split := quickjsRandom{state: 42 ^ 0x9e3779b9}
	other := make([]byte, 8)
	split.Read(other[:3])
	split.Read(other[3:])
	for i := range got {
		if got[i] != other[i] {
			t.Fatal("chunk dependent WASI random")
		}
	}
}
