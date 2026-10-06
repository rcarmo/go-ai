package durable

import (
	"strings"
	"unicode/utf8"
)

// outputDecoder follows UTF-8 TextDecoder replacement/streaming rules. Only an
// incomplete scalar (at most three bytes) is retained between byte chunks.
type outputDecoder struct {
	pending string
	started bool
}

func (d *outputDecoder) decode(chunk []byte, end bool) string {
	data := d.pending + string(chunk)
	d.pending = ""
	var out strings.Builder
	for i := 0; i < len(data); {
		b := data[i]
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
			d.started = true
			i++
			continue
		}
		consumed := 1
		valid := true
		for consumed < size {
			if i+consumed >= len(data) {
				if !end {
					d.pending = data[i:]
					return out.String()
				}
				valid = false
				break
			}
			c := data[i+consumed]
			if c < 0x80 || c > 0xbf || consumed == 1 && (b == 0xe0 && c < 0xa0 || b == 0xed && c > 0x9f || b == 0xf0 && c < 0x90 || b == 0xf4 && c > 0x8f) {
				valid = false
				break
			}
			consumed++
		}
		if !valid {
			out.WriteRune(utf8.RuneError)
			d.started = true
			i += consumed
			continue
		}
		scalar := data[i : i+size]
		// The default TextDecoder suppresses the first decoded BOM of each stream.
		if d.started || scalar != "\ufeff" {
			out.WriteString(scalar)
		}
		d.started = true
		i += size
	}
	return out.String()
}
func (d *outputDecoder) end() string {
	output := d.decode(nil, true)
	*d = outputDecoder{}
	return output
}
