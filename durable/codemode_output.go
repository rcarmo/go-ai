package durable

import (
	"unicode/utf8"
)

const MaxCodemodeFullOutputBytes = 4 << 20

func validateCodemodeFullText(text string) error {
	if len(text) > MaxCodemodeFullOutputBytes || !utf8.ValidString(text) {
		return reject("invalid or oversized codemode full output")
	}
	return nil
}

// Saturate at platform int capacity; parser-safe token values can still
// overflow a four-unit estimate on narrower hosts.
func codemodeTokenUnits(tokens int) int {
	if tokens <= 0 {
		return 0
	}
	max := int(^uint(0) >> 1)
	if tokens > max/4 {
		return max
	}
	return tokens * 4
}

// Native serial candidate uses the pinned four-character/token estimate. It
// retains a UTF8-safe prefix; full head/tail/spill rendering is separate.
func boundCodemodeOutput(text string, tokens int) string {
	if !utf8.ValidString(text) {
		text = "Invalid sandbox output"
	}
	if tokens <= 0 {
		return ""
	}
	budget := codemodeTokenUnits(tokens)
	units := 0
	for pos, r := range text {
		count := 1
		if r > 0xffff {
			count = 2
		}
		if units+count > budget {
			return text[:pos]
		}
		units += count
	}
	return text
}
