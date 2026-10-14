package quickjs

// quickjsRandom matches pinned vm.ts mulberry32 byte generation. Each WASI
// output byte consumes one generator step, independently of read chunking.
type quickjsRandom struct{ state uint32 }

func (r *quickjsRandom) Read(p []byte) (int, error) {
	for i := range p {
		r.state += 0x6d2b79f5
		t := r.state
		t = (t ^ (t >> 15)) * (t | 1)
		t ^= t + ((t ^ (t >> 7)) * (t | 61))
		p[i] = byte((t ^ (t >> 14)) >> 24)
	}
	return len(p), nil
}
