package quickjs

import (
	"reflect"
	"testing"
)

// Independent pinned TypeScript prng/random_get reference: words are packed
// little-endian, with unused final bytes discarded at each call boundary.
func TestQuickJS120MulberryWASIReference(t *testing.T) {
	for _, tc := range []struct {
		name  string
		sizes []int
		want  [][]byte
	}{
		{"whole", []int{8}, [][]byte{{4, 136, 55, 123, 2, 149, 57, 252}}},
		{"word-split", []int{4, 4}, [][]byte{{4, 136, 55, 123}, {2, 149, 57, 252}}},
		{"partial-split", []int{3, 5}, [][]byte{{4, 136, 55}, {2, 149, 57, 252, 22}}},
		{"empty-and-partials", []int{0, 1, 2, 3, 4}, [][]byte{{}, {4}, {2, 149}, {22, 36, 232}, {0, 234, 241, 156}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := quickjsRandom{state: 42 ^ 0x9e3779b9}
			for i, size := range tc.sizes {
				got := make([]byte, size)
				n, err := r.Read(got)
				if err != nil || n != size || !reflect.DeepEqual(got, tc.want[i]) {
					t.Fatal("WASI PRNG mismatch", got, tc.want[i], n, err)
				}
			}
		})
	}
}
