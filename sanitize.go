// Unicode sanitization — removes unpaired surrogates that cause JSON errors.
package goai

import "strings"

// SanitizeSurrogates removes unpaired Unicode surrogate characters from a string.
// Valid emoji and other properly paired surrogates are preserved.
//
// Go strings are UTF-8, so unpaired surrogates are represented as the
// replacement character U+FFFD or as invalid byte sequences. This function
// removes those invalid sequences.
func SanitizeSurrogates(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		// U+FFFD is the replacement character used for invalid UTF-8
		// Skip surrogates in the D800-DFFF range (shouldn't appear in valid Go strings
		// but can appear from broken input)
		if r == 0xFFFD {
			continue
		}
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
