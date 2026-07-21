package goai

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// ContentText extracts and joins text blocks from message content.
func ContentText(content interface{}, separators ...string) string {
	separator := "\n"
	if len(separators) > 0 {
		separator = separators[0]
	}
	switch v := content.(type) {
	case string:
		return v
	case []ContentBlock:
		return contentBlocksText(v, separator)
	case []*ContentBlock:
		blocks := make([]ContentBlock, 0, len(v))
		for _, block := range v {
			if block != nil {
				blocks = append(blocks, *block)
			}
		}
		return contentBlocksText(blocks, separator)
	default:
		return ""
	}
}

func contentBlocksText(blocks []ContentBlock, separator string) string {
	out := ""
	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if out != "" {
			out += separator
		}
		out += block.Text
	}
	return out
}

var uuidV7State = struct {
	sync.Mutex
	lastTimestamp int64
	sequence      uint32
	now           func() int64
	random        func([]byte) error
}{
	lastTimestamp: -1,
	now:           func() int64 { return time.Now().UnixMilli() },
	random: func(dst []byte) error {
		_, err := rand.Read(dst)
		return err
	},
}

// UUIDv7 generates a time-ordered RFC 9562 UUIDv7 string.
func UUIDv7() string {
	var random [16]byte
	if err := uuidV7State.random(random[:]); err != nil {
		now := time.Now().UnixNano()
		for i := range random {
			random[i] = byte(now >> (uint(i%8) * 8))
		}
	}
	timestamp := uuidV7State.now()

	uuidV7State.Lock()
	if timestamp > uuidV7State.lastTimestamp {
		uuidV7State.sequence = uint32(random[6])<<24 | uint32(random[7])<<16 | uint32(random[8])<<8 | uint32(random[9])
		uuidV7State.lastTimestamp = timestamp
	} else {
		uuidV7State.sequence++
		if uuidV7State.sequence == 0 {
			uuidV7State.lastTimestamp++
		}
	}
	lastTimestamp := uuidV7State.lastTimestamp
	sequence := uuidV7State.sequence
	uuidV7State.Unlock()

	var bytes [16]byte
	bytes[0] = byte(lastTimestamp >> 40)
	bytes[1] = byte(lastTimestamp >> 32)
	bytes[2] = byte(lastTimestamp >> 24)
	bytes[3] = byte(lastTimestamp >> 16)
	bytes[4] = byte(lastTimestamp >> 8)
	bytes[5] = byte(lastTimestamp)
	bytes[6] = 0x70 | byte(sequence>>28)&0x0f
	bytes[7] = byte(sequence >> 20)
	bytes[8] = 0x80 | byte(sequence>>14)&0x3f
	bytes[9] = byte(sequence >> 6)
	bytes[10] = byte(sequence&0x3f)<<2 | (random[10] & 0x03)
	copy(bytes[11:], random[11:])
	hexBytes := make([]byte, 32)
	hex.Encode(hexBytes, bytes[:])
	return string(hexBytes[0:8]) + "-" + string(hexBytes[8:12]) + "-" + string(hexBytes[12:16]) + "-" + string(hexBytes[16:20]) + "-" + string(hexBytes[20:32])
}
