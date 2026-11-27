package goai

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"golang.org/x/net/idna"
	"golang.org/x/text/unicode/norm"
)

var schemaIDNHostnameProfile = idna.New(
	idna.MapForLookup(),
	idna.BidiRule(),
	idna.VerifyDNSLength(false),
	idna.StrictDomainName(true),
	idna.ValidateLabels(true),
	idna.CheckHyphens(true),
	idna.CheckJoiners(true),
	idna.Transitional(false),
)

// This follows the pinned TypeBox IDNA hostname predicate rather than the
// different x/net lookup profile. x/net supplies table-backed Unicode/ACE
// conversion, while the contextual and code-unit limits below retain the
// prepared Schema fixture's exact contract.
func schemaFormatIDNHostname(value string) bool {
	if value == "" || strings.Contains(value, " ") {
		return false
	}
	var normalized strings.Builder
	for _, r := range value {
		switch {
		case r >= '\uff01' && r <= '\uff5e':
			r -= 0xfee0
		case r == '\u3002' || r == '\uff0e' || r == '\uff61':
			r = '.'
		case isSchemaIDNHostnameIgnored(r):
			continue
		}
		normalized.WriteRune(r)
	}
	value = norm.NFC.String(normalized.String())
	if len(utf16.Encode([]rune(value))) > 253 {
		return false
	}
	labels := strings.Split(value, ".")
	decodedLabels := make([]string, len(labels))
	for i, label := range labels {
		if label == "" || len(utf16.Encode([]rune(label))) > 63 {
			return false
		}
		decoded := label
		if strings.HasPrefix(strings.ToLower(label), "xn--") {
			var err error
			decoded, err = schemaIDNHostnameProfile.ToUnicode(label)
			if err != nil || !hasSchemaIDNHostnameNonASCII(decoded) {
				return false
			}
		} else if hasSchemaIDNHostnameNonASCII(label) && len(schemaIDNHostnamePunycode(label))+4 > 63 {
			return false
		}
		if !validSchemaIDNHostnameUnicodeLabel(decoded) {
			return false
		}
		decodedLabels[i] = decoded
	}
	hasBidi := false
	for _, label := range decodedLabels {
		if schemaIDNHostnameHasRTL(label) {
			hasBidi = true
			break
		}
	}
	if hasBidi {
		for _, label := range decodedLabels {
			if !schemaIDNHostnameSatisfiesBidi(label) {
				return false
			}
		}
	}
	return true
}

func isSchemaIDNHostnameIgnored(r rune) bool {
	return r == '\u00ad' || r == '\u034f' || (r >= '\u180b' && r <= '\u180d') || r == '\u200b' ||
		(r >= '\ufe00' && r <= '\ufe0f') || (r >= '\U000e0100' && r <= '\U000e01ef')
}

func hasSchemaIDNHostnameNonASCII(value string) bool {
	for _, r := range value {
		if r >= 0x80 {
			return true
		}
	}
	return false
}

func validSchemaIDNHostnameUnicodeLabel(value string) bool {
	runes := []rune(value)
	if len(runes) == 0 || runes[0] == '-' || runes[len(runes)-1] == '-' || (len(runes) > 3 && runes[2] == '-' && runes[3] == '-') {
		return false
	}
	if unicode.Is(unicode.Mn, runes[0]) || unicode.Is(unicode.Mc, runes[0]) || unicode.Is(unicode.Me, runes[0]) {
		return false
	}
	for i, r := range runes {
		if schemaIDNHostnameDisallowed(r) || !schemaIDNHostnamePermitted(r) {
			return false
		}
		switch r {
		case '\u00b7':
			if i == 0 || i+1 == len(runes) || runes[i-1] != 'l' || runes[i+1] != 'l' {
				return false
			}
		case '\u0375':
			if i+1 == len(runes) || !unicode.Is(unicode.Greek, runes[i+1]) {
				return false
			}
		case '\u05f3', '\u05f4':
			if i == 0 || !unicode.Is(unicode.Hebrew, runes[i-1]) {
				return false
			}
		case '\u200c':
			if i == 0 || (runes[i-1] < 0x80 && !schemaIDNHostnameVirama(runes[i-1])) {
				return false
			}
		case '\u200d':
			if i == 0 || !schemaIDNHostnameVirama(runes[i-1]) {
				return false
			}
		}
	}
	if strings.ContainsRune(value, '\u30fb') {
		for _, r := range runes {
			if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Han, r) {
				return true
			}
		}
		return false
	}
	return true
}

func schemaIDNHostnamePermitted(r rune) bool {
	if unicode.IsLetter(r) || unicode.Is(unicode.Nd, r) || unicode.IsMark(r) || r == '-' || r == '+' || r == '.' || r == ',' || r == ':' || r == '/' {
		return true
	}
	switch r {
	case '\u00b7', '\u0375', '\u05f3', '\u05f4', '\u200c', '\u200d', '\u30fb', '\u00df', '\u03c2', '\u06fd', '\u06fe', '\u0f0b', '\u3007':
		return true
	}
	return false
}

func schemaIDNHostnameDisallowed(r rune) bool {
	switch r {
	case '\u0640', '\u07fa', '\u302e', '\u302f', '\u3031', '\u3032', '\u3033', '\u3034', '\u3035', '\u303b':
		return true
	}
	return false
}

func schemaIDNHostnameBidiClass(r rune) string {
	switch {
	case r >= '0' && r <= '9', r >= '\u06f0' && r <= '\u06f9':
		return "EN"
	case r >= '\u0660' && r <= '\u0669':
		return "AN"
	case unicode.Is(unicode.Mn, r):
		return "NSM"
	case unicode.Is(unicode.Hebrew, r):
		return "R"
	case unicode.Is(unicode.Arabic, r) || unicode.Is(unicode.Syriac, r) || unicode.Is(unicode.Thaana, r) || unicode.Is(unicode.Mandaic, r):
		return "AL"
	case unicode.IsLetter(r):
		return "L"
	default:
		return "ON"
	}
}

func schemaIDNHostnameHasRTL(value string) bool {
	for _, r := range value {
		switch schemaIDNHostnameBidiClass(r) {
		case "R", "AL", "AN":
			return true
		}
	}
	return false
}

func schemaIDNHostnameSatisfiesBidi(value string) bool {
	runes := []rune(value)
	if len(runes) == 0 {
		return false
	}
	first := schemaIDNHostnameBidiClass(runes[0])
	if first != "L" && first != "R" && first != "AL" {
		return false
	}
	rtl := first == "R" || first == "AL"
	seenEN, seenAN := false, false
	for _, r := range runes {
		class := schemaIDNHostnameBidiClass(r)
		if rtl {
			switch class {
			case "R", "AL", "AN", "EN", "ON", "NSM":
			default:
				return false
			}
		} else {
			switch class {
			case "L", "EN", "ON", "NSM":
			default:
				return false
			}
		}
		seenEN = seenEN || class == "EN"
		seenAN = seenAN || class == "AN"
	}
	return !rtl || !seenEN || !seenAN
}

// TypeBox's RFC 3492 encoder is only needed here to apply the 63-octet
// A-label limit to U-labels whose characters x/net cannot convert literally.
func schemaIDNHostnamePunycode(input string) string {
	const base, tMin, tMax, skew, damp, initialBias, initialN = 36, 1, 26, 38, 700, 72, 128
	basic := make([]rune, 0, len(input))
	points := []rune(input)
	for _, r := range points {
		if r < 0x80 {
			basic = append(basic, r)
		}
	}
	result := append([]rune(nil), basic...)
	if len(basic) > 0 {
		result = append(result, '-')
	}
	adapt := func(delta, count int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / count
		k := 0
		for delta > ((base-tMin)*tMax)/2 {
			delta /= base - tMin
			k += base
		}
		return k + ((base-tMin+1)*delta)/(delta+skew)
	}
	n, delta, bias, handled := initialN, 0, initialBias, len(basic)
	for handled < len(points) {
		m := int(^uint(0) >> 1)
		for _, cp := range points {
			if int(cp) >= n && int(cp) < m {
				m = int(cp)
			}
		}
		delta += (m - n) * (handled + 1)
		n = m
		for _, cp := range points {
			if int(cp) < n {
				delta++
			}
			if int(cp) == n {
				q := delta
				for k := base; ; k += base {
					t := tMin
					if k <= bias {
						t = tMin
					} else if k >= bias+tMax {
						t = tMax
					} else {
						t = k - bias
					}
					if q < t {
						break
					}
					digit := t + ((q - t) % (base - t))
					result = append(result, schemaIDNHostnamePunyDigit(digit))
					q = (q - t) / (base - t)
				}
				result = append(result, schemaIDNHostnamePunyDigit(q))
				bias = adapt(delta, handled+1, handled == len(basic))
				delta = 0
				handled++
			}
		}
		delta++
		n++
	}
	return string(result)
}

func schemaIDNHostnamePunyDigit(digit int) rune {
	if digit < 26 {
		return rune('a' + digit)
	}
	return rune('0' + digit - 26)
}

func schemaIDNHostnameVirama(r rune) bool {
	switch r {
	case '\u094d', '\u09cd', '\u0a4d', '\u0acd', '\u0b4d', '\u0bcd', '\u0c4d', '\u0ccd', '\u0d3b', '\u0d3c', '\u0d4d', '\u0dca', '\u1b44', '\u1baa', '\u1bab', '\ua9c0', '\U00011046', '\U0001107f', '\U000110b9', '\U00011133', '\U00011134', '\U000111c0', '\U00011235', '\U0001134d', '\U00011442', '\U000114c2', '\U000115bf', '\U0001163f', '\U000116b6', '\U00011c3f', '\U00011d44', '\U00011d45':
		return true
	}
	return false
}
