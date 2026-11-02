package goai

// Hostname/IDNA rules adapted from the pinned TypeBox implementation (MIT),
// Copyright (c) 2017-2026 Haydn Paterson. See THIRD_PARTY_NOTICES.md.
// Do not substitute a stricter IDNA profile: upstream intentionally has no NFC
// check, permits some context forms, and does not enforce the final bidi class.
import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func schemaFormatHostname(value string) bool {
	if value == "" || !schemaHostnameLength(value, 253) || value[len(value)-1] == '.' {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || !schemaHostnameLength(label, 63) {
			return false
		}
		if len(label) >= 4 && (label[0] == 'x' || label[0] == 'X') && (label[1] == 'n' || label[1] == 'N') && label[2:4] == "--" {
			body := label[4:]
			// JS lowercasing is Unicode, including Kelvin sign and dotted I.
			// Casers may be stateful; never share one between validation calls.
			if strings.IndexFunc(body, func(r rune) bool { return r >= utf8.RuneSelf }) >= 0 {
				body = cases.Lower(language.Und).String(body)
			} else {
				body = schemaFormatASCIIToLower(body)
			}
			if strings.LastIndexByte(body, '-') == 0 {
				return false
			}
			decoded, ok := schemaPunyDecode(body)
			if !ok || !schemaHostnameUnicodeLabel(decoded) {
				return false
			}
			continue
		}
		if label[0] == '-' || label[len(label)-1] == '-' || (len(label) >= 4 && label[2:4] == "--") {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

// Upstream lengths are UTF-16 units, not UTF-8 bytes; non-ASCII ACE payload
// casing can still yield a valid label. Stop once the bounded limit is exceeded.
func schemaHostnameLength(value string, limit int) bool {
	n := 0
	for _, r := range value {
		n++
		if r > 0xffff {
			n++
		}
		if n > limit {
			return false
		}
	}
	return true
}

func schemaPunyThreshold(k, bias int64) int64 {
	if k <= bias {
		return 1
	}
	if k >= bias+26 {
		return 26
	}
	return k - bias
}

func schemaPunyAdapt(delta, points int64, first bool) int64 {
	if first {
		delta /= 700
	} else {
		delta /= 2
	}
	delta += delta / points
	k := int64(0)
	for delta > 455 {
		delta /= 35
		k += 36
	}
	return k + 36*delta/(delta+38)
}

func schemaPunyDecode(body string) ([]rune, bool) {
	var out []rune
	pos := 0
	if delimiter := strings.LastIndexByte(body, '-'); delimiter >= 0 {
		for j := 0; j < delimiter; j++ {
			if body[j] >= utf8.RuneSelf {
				return nil, false
			}
			out = append(out, rune(body[j]))
		}
		pos = delimiter + 1
	}
	n, index, bias := int64(128), int64(0), int64(72)
	for pos < len(body) {
		old, weight := index, int64(1)
		// Any larger insertion index necessarily decodes beyond MaxRune.
		// This also bounds arithmetic before hostile payloads can overflow.
		limit := int64(utf8.MaxRune) * int64(len(out)+1)
		for k := int64(36); ; k += 36 {
			if pos == len(body) {
				return nil, false
			}
			c := body[pos]
			pos++
			var digit int64
			switch {
			case c >= 'a' && c <= 'z':
				digit = int64(c - 'a')
			case c >= '0' && c <= '9':
				digit = int64(c-'0') + 26
			default:
				return nil, false
			}
			if digit != 0 && weight > (limit-index)/digit {
				return nil, false
			}
			index += digit * weight
			t := schemaPunyThreshold(k, bias)
			if digit < t {
				break
			}
			weight *= 36 - t // Bounded by the preceding insertion-index check.
		}
		length := int64(len(out) + 1)
		bias = schemaPunyAdapt(index-old, length, old == 0)
		n += index / length
		if n > utf8.MaxRune || n >= 0xd800 && n <= 0xdfff {
			return nil, false
		}
		index %= length
		at := int(index)
		out = append(out, 0)
		copy(out[at+1:], out[at:len(out)-1])
		out[at] = rune(n)
		index++
	}
	return out, true
}

// Only the encoded length is needed, not an allocated canonical A-label.
func schemaPunyLengthOK(chars []rune) bool {
	basic := 0
	for _, r := range chars {
		if r < utf8.RuneSelf {
			basic++
		}
	}
	if basic == len(chars) {
		return false // An ACE label must contain a non-ASCII character.
	}
	length := basic
	if basic != 0 {
		length++
	}
	n, delta, bias := int64(128), int64(0), int64(72)
	for handled := basic; handled < len(chars); {
		m := int64(utf8.MaxRune)
		for _, r := range chars {
			if int64(r) >= n && int64(r) < m {
				m = int64(r)
			}
		}
		delta += (m - n) * int64(handled+1)
		n = m
		for _, r := range chars {
			if int64(r) < n {
				delta++
			}
			if int64(r) != n {
				continue
			}
			q := delta
			for k := int64(36); ; k += 36 {
				t := schemaPunyThreshold(k, bias)
				if q < t {
					break
				}
				length++
				q = (q - t) / (36 - t)
			}
			length++
			if length+4 > 63 {
				return false
			}
			bias = schemaPunyAdapt(delta, int64(handled+1), handled == basic)
			delta = 0
			handled++
		}
		delta++
		n++
	}
	return length+4 <= 63
}

func schemaHostnameUnicodeLabel(chars []rune) bool {
	if len(chars) == 0 || !schemaPunyLengthOK(chars) || !schemaHostnameBidi(chars) ||
		chars[0] == '-' || chars[len(chars)-1] == '-' || len(chars) >= 4 && chars[2] == '-' && chars[3] == '-' || unicode.IsMark(chars[0]) {
		return false
	}
	japanese, middleDot := false, false
	for i, r := range chars {
		switch r {
		case 0x0640, 0x07fa, 0x302e, 0x302f, 0x3031, 0x3032, 0x3033, 0x3034, 0x3035, 0x303b:
			return false
		}
		if !schemaHostnamePermitted(r) {
			return false
		}
		japanese = japanese || unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Han)
		switch r {
		case 0x00b7:
			if i == 0 || i+1 == len(chars) || chars[i-1] != 'l' || chars[i+1] != 'l' {
				return false
			}
		case 0x0375:
			if i+1 == len(chars) || !unicode.Is(unicode.Greek, chars[i+1]) {
				return false
			}
		case 0x05f3, 0x05f4:
			if i == 0 || !unicode.Is(unicode.Hebrew, chars[i-1]) {
				return false
			}
		case 0x200c:
			if i == 0 || chars[i-1] < 0x80 && !schemaHostnameVirama(chars[i-1]) {
				return false
			}
		case 0x200d:
			if i == 0 || !schemaHostnameVirama(chars[i-1]) {
				return false
			}
		case 0x30fb:
			middleDot = true
		}
	}
	return !middleDot || japanese
}

func schemaHostnamePermitted(r rune) bool {
	if unicode.IsLetter(r) || unicode.In(r, unicode.Nd, unicode.Mn, unicode.Mc) {
		return true
	}
	switch r {
	case '-', '+', '.', ',', ':', '/', 0x00b7, 0x0375, 0x05f3, 0x05f4, 0x200c, 0x200d, 0x30fb,
		0x00df, 0x03c2, 0x06fd, 0x06fe, 0x0f0b, 0x3007:
		return true
	}
	return false
}

func schemaHostnameVirama(r rune) bool {
	switch r {
	case 0x094d, 0x09cd, 0x0a4d, 0x0acd, 0x0b4d, 0x0bcd, 0x0c4d, 0x0ccd, 0x0d3b, 0x0d3c, 0x0d4d, 0x0dca,
		0x1b44, 0x1baa, 0x1bab, 0xa9c0, 0x11046, 0x1107f, 0x110b9, 0x11133, 0x11134, 0x111c0, 0x11235,
		0x1134d, 0x11442, 0x114c2, 0x115bf, 0x1163f, 0x116b6, 0x11c3f, 0x11d44, 0x11d45:
		return true
	}
	return false
}

func schemaHostnameBidiClass(r rune) byte {
	switch {
	case r >= '0' && r <= '9' || r >= 0x06f0 && r <= 0x06f9:
		return 'E'
	case r >= 0x0660 && r <= 0x0669:
		return 'A'
	case unicode.Is(unicode.Mn, r):
		return 'N'
	case unicode.Is(unicode.Hebrew, r):
		return 'R'
	case unicode.In(r, unicode.Arabic, unicode.Syriac, unicode.Thaana, unicode.Mandaic):
		return 'B'
	case unicode.IsLetter(r):
		return 'L'
	default:
		return 'O'
	}
}

func schemaHostnameBidi(chars []rune) bool {
	hasRTL := false
	for _, r := range chars {
		c := schemaHostnameBidiClass(r)
		hasRTL = hasRTL || c == 'R' || c == 'B' || c == 'A'
	}
	if !hasRTL {
		return true
	}
	first := schemaHostnameBidiClass(chars[0])
	if first != 'L' && first != 'R' && first != 'B' {
		return false
	}
	rtl, en, an := first != 'L', false, false
	for _, r := range chars {
		c := schemaHostnameBidiClass(r)
		if rtl && c == 'L' || !rtl && (c == 'R' || c == 'B' || c == 'A') {
			return false
		}
		en, an = en || c == 'E', an || c == 'A'
	}
	return !rtl || !en || !an
}
