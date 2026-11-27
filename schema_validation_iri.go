package goai

import (
	"regexp"
	"strings"
)

// These checks follow TypeBox's pinned iri.ts and iri_reference.ts (MIT,
// Copyright (c) 2017-2026 Haydn Paterson; see THIRD_PARTY_NOTICES.md).
var schemaIRIIPvFuture = regexp.MustCompile(`\[[vV][0-9a-fA-F]+\.[^\]]+\]`)

func schemaFormatIRI(value any, text string) bool {
	if !schemaIRITextOK(text, "<>^`{|}") {
		return false
	}
	// The pinned absolute IRI format substitutes only the first matching
	// IPvFuture literal and only below 2048 JS UTF-16 code units.
	if schemaIRIShortEnoughForIPvFuture(value, text) {
		if span := schemaIRIIPvFuture.FindStringIndex(text); span != nil {
			text = text[:span[0]] + "[::1]" + text[span[1]:]
		}
	}
	_, err := parseSchemaURL("", text)
	return err == nil
}

func schemaFormatIRIReference(text string) bool {
	if !schemaIRITextOK(text, "\x7f") || schemaIRIMalformedScheme(text) {
		return false
	}
	_, err := parseSchemaURL("http://example.com", text)
	return err == nil
}

func schemaIRIShortEnoughForIPvFuture(value any, text string) bool {
	const limit = 2048
	if len(text) < limit {
		return true
	}
	// A UTF-16 code unit accounts for at most three UTF-8 bytes.
	if len(text) >= limit*3 {
		return false
	}
	if exceptional, ok := value.(UTF16String); ok {
		return len(exceptional.CodeUnits()) < limit
	}
	units := 0
	for _, r := range text {
		units++
		if r > 0xffff {
			units++
		}
		if units >= limit {
			return false
		}
	}
	return true
}

// Check the text before the URL parser, which may normalize characters the
// format must reject. Each format supplies its additional forbidden bytes.
func schemaIRITextOK(text, forbidden string) bool {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c <= 0x20 || c == '\\' || strings.IndexByte(forbidden, c) >= 0 {
			return false
		}
		if c == '%' {
			if i+2 >= len(text) || !schemaIRIHex(text[i+1]) || !schemaIRIHex(text[i+2]) {
				return false
			}
			i += 2
		}
	}
	return true
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
