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

// Native serial candidate uses the pinned four-character/token estimate. It
// retains a UTF8-safe prefix; full head/tail/spill rendering is separate.
func boundCodemodeOutput(text string, tokens int) string {
	if !utf8.ValidString(text) {
		text = "Invalid sandbox output"
	}
	if tokens <= 0 {
		return ""
	}
	if uint64(tokens) > MaxID/4 {
		return text
	}
	budget := tokens * 4
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
