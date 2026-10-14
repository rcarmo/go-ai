package tools

import (
	"bytes"
	"encoding/binary"
)

// APNG is deliberately excluded by pinned image detection. Animated WebP is
// inline-compatible; decoding validates the raster or rejects unsupported data.
func animatedPNG(data []byte) bool {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		return false
	}
	for pos := uint64(8); pos+12 <= uint64(len(data)); {
		size := uint64(binary.BigEndian.Uint32(data[pos : pos+4]))
		end := pos + 12 + size
		if end > uint64(len(data)) {
			return false
		}
		kind := string(data[pos+4 : pos+8])
		if kind == "acTL" {
			return true
		}
		if kind == "IDAT" || kind == "IEND" {
			return false
		}
		pos = end
	}
	return false
}
