package tools

import (
	"bytes"
	"context"
	"encoding/binary"

	"github.com/rcarmo/go-ai/durable"
)

// detectImageMimeOf matches the pinned file-backed detector. Only PNG chunk
// headers before acTL/IDAT need additional reads; payloads are skipped and each
// cached block is at most 64KiB, independent of image/file size.
func detectImageMimeOf(ctx context.Context, reader durable.BinaryReader, size int64, header []byte) (string, error) {
	mime := detectImageMime(header)
	if mime != "image/png" {
		return mime, nil
	}
	var block []byte
	var blockStart int64
	for offset := int64(8); offset <= size-8; {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if offset < blockStart || offset+8 > blockStart+int64(len(block)) {
			blockStart = offset
			var err error
			block, err = reader.Read(ctx, offset, int(min(int64(64<<10), size-offset)))
			if err != nil {
				return "", err
			}
		}
		at := int(offset - blockStart)
		if len(block)-at < 8 {
			break
		}
		chunk := block[at : at+8]
		switch string(chunk[4:]) {
		case "acTL":
			return "", nil
		case "IDAT":
			return "image/png", nil
		}
		next := offset + 12 + int64(binary.BigEndian.Uint32(chunk))
		if next <= offset || next > size {
			break
		}
		offset = next
	}
	return "image/png", nil
}

// detectImageMime follows the pinned tools/image.ts detector, including APNG
// rejection and BMP structural checks. Detection is not image decoding.
func detectImageMime(data []byte) string {
	ascii := func(offset int, text string) bool {
		return offset >= 0 && len(data) >= offset+len(text) && string(data[offset:offset+len(text)]) == text
	}
	byteAt := func(index int) uint32 {
		if index >= len(data) {
			return 0
		}
		return uint32(data[index])
	}
	le16 := func(offset int) uint32 { return byteAt(offset) | byteAt(offset+1)<<8 }
	le32 := func(offset int) uint32 {
		return byteAt(offset) | byteAt(offset+1)<<8 | byteAt(offset+2)<<16 | byteAt(offset+3)<<24
	}
	be32 := func(offset int) uint32 {
		return byteAt(offset)<<24 | byteAt(offset+1)<<16 | byteAt(offset+2)<<8 | byteAt(offset+3)
	}
	if bytes.HasPrefix(data, []byte{255, 216, 255}) {
		if byteAt(3) != 0xf7 {
			return "image/jpeg"
		}
		return ""
	}
	if bytes.HasPrefix(data, []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		if len(data) < 16 || be32(8) != 13 || !ascii(12, "IHDR") {
			return ""
		}
		for offset := uint64(8); offset+8 <= uint64(len(data)); {
			if ascii(int(offset)+4, "acTL") {
				return ""
			}
			if ascii(int(offset)+4, "IDAT") {
				return "image/png"
			}
			next := offset + 12 + uint64(be32(int(offset)))
			if next <= offset || next > uint64(len(data)) {
				break
			}
			offset = next
		}
		return "image/png"
	}
	if ascii(0, "GIF87a") || ascii(0, "GIF89a") {
		return "image/gif"
	}
	if ascii(0, "RIFF") && ascii(8, "WEBP") {
		return "image/webp"
	}
	if ascii(0, "BM") && len(data) >= 26 {
		size, pixels, dib := uint64(le32(2)), uint64(le32(10)), uint64(le32(14))
		if size != 0 && size < 26 || pixels < 14+dib || size != 0 && pixels >= size {
			return ""
		}
		planes, bits := uint32(0), uint32(0)
		if dib == 12 {
			planes, bits = le16(22), le16(24)
		} else if dib >= 40 && dib <= 124 && len(data) >= 30 {
			planes, bits = le16(26), le16(28)
		} else {
			return ""
		}
		if planes == 1 {
			switch bits {
			case 1, 4, 8, 16, 24, 32:
				return "image/bmp"
			}
		}
	}
	return ""
}
