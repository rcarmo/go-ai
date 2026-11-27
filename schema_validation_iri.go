package goai

import "strings"

// The guards follow TypeBox's pinned iri_reference.ts (MIT, Copyright (c)
// 2017-2026 Haydn Paterson; see THIRD_PARTY_NOTICES.md). Check them before the
// URL parser, which can normalize characters the format must reject.
func schemaFormatIRIReference(text string) bool {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c <= 0x20 || c == 0x7f || c == '\\' {
			return false
		}
		if c == '%' {
			if i+2 >= len(text) || !schemaIRIHex(text[i+1]) || !schemaIRIHex(text[i+2]) {
				return false
			}
			i += 2
		}
	}
	if schemaIRIMalformedScheme(text) {
		return false
	}
	_, err := parseSchemaURL("http://example.com", text)
	return err == nil
}

func schemaIRIHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// A leading ASCII scheme token immediately followed by // lacks its colon.
// A relative path or a token beginning with a digit is not such a scheme.
func schemaIRIMalformedScheme(text string) bool {
	letter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	if len(text) == 0 || !letter(text[0]) {
		return false
	}
	i := 1
	for i < len(text) {
		c := text[i]
		if !letter(c) && !(c >= '0' && c <= '9') && c != '+' && c != '-' && c != '.' {
			break
		}
		i++
	}
	return strings.HasPrefix(text[i:], "//")
}
