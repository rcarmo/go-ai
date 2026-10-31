// Package errortext handles network diagnostics, not argument/DTO validation.
package errortext

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

func Trim(text string) string {
	return strings.TrimFunc(text, func(r rune) bool { return r == '\ufeff' || r != '\u0085' && unicode.IsSpace(r) })
}

// Decode a complete admitted prefix with Response.text()/UTF-8 TextDecoder
// replacement rules. Incomplete valid prefixes produce one replacement unit.
func DecodeUTF8(raw []byte) string {
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if utf8.Valid(raw) {
		return string(raw)
	}
	var out strings.Builder
	for i := 0; i < len(raw); {
		b := raw[i]
		size := 0
		switch {
		case b < 0x80:
			size = 1
		case b >= 0xc2 && b <= 0xdf:
			size = 2
		case b >= 0xe0 && b <= 0xef:
			size = 3
		case b >= 0xf0 && b <= 0xf4:
			size = 4
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		consumed := 1
		valid := true
		for consumed < size {
			if i+consumed >= len(raw) {
				valid = false
				break
			}
			c := raw[i+consumed]
			if c < 0x80 || c > 0xbf || consumed == 1 && (b == 0xe0 && c < 0xa0 || b == 0xed && c > 0x9f || b == 0xf0 && c < 0x90 || b == 0xf4 && c > 0x8f) {
				valid = false
				break
			}
			consumed++
		}
		if !valid {
			out.WriteRune(utf8.RuneError)
			i += consumed
			continue
		}
		out.Write(raw[i : i+size])
		i += size
	}
	return out.String()
}
