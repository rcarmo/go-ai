package quickjs

// quickjsRandom matches pinned vm.ts random_get: each Read consumes one
// mulberry32 word per four bytes, little-endian. A partial final word discards
// its unused bytes, matching one WASI random_get invocation.
type quickjsRandom struct{ state uint32 }

func (r *quickjsRandom) Read(p []byte) (int, error) {
	for i := 0; i < len(p); i += 4 {
		r.state += 0x6d2b79f5
		t := r.state
		t = (t ^ (t >> 15)) * (t | 1)
		t ^= t + ((t ^ (t >> 7)) * (t | 61))
		word := t ^ (t >> 14)
		for j := 0; j < 4 && i+j < len(p); j++ {
			p[i+j] = byte(word >> (8 * j))
		}
	}
	return len(p), nil
}
