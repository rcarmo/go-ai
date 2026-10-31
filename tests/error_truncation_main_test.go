package goai_test

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
)

func TestErrorTruncationUTF16Main(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		limit      int
		want       string
	}{
		{"ascii", "abcd", 2, "ab... [truncated 2 chars]"},
		{"bmp-within-limit", "é界", 2, "é界"},
		{"bmp-truncated", "é界x", 2, "é界... [truncated 1 chars]"},
		{"astral-within-limit", "😀x", 3, "😀x"},
		{"astral-truncated", "😀xy", 2, "😀... [truncated 2 chars]"},
		{"combining", "e\u0301x", 2, "e\u0301... [truncated 1 chars]"},
		{"empty", "", 0, ""},
		{"zero", "abc", 0, "... [truncated 3 chars]"},
		// Native ordinary error strings stay scalar-valid. JS slice can leave an
		// isolated surrogate at these boundaries; that difference awaits acceptance.
		{"split-pair", "a😀z", 2, "a... [truncated 3 chars]"},
		{"pair-at-start", "😀x", 1, "... [truncated 3 chars]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := goai.TruncateErrorText(tc.text, tc.limit)
			if got != tc.want || !utf8.ValidString(got) {
				t.Fatal("wrong scalar-safe UTF16 truncation", got, tc.want)
			}
		})
	}
}

func TestProviderErrorUnicodeCapMain(t *testing.T) {
	body := strings.Repeat("界", goai.MaxProviderErrorBodyChars+3)
	norm := goai.NormalizeProviderError(providerErr{status: 403, hasStatus: true, body: body, hasBody: true, msg: "blocked"})
	want := strings.Repeat("界", goai.MaxProviderErrorBodyChars) + "... [truncated 3 chars]"
	if norm.Body != want || !norm.HasBody || norm.MessageCarriesBody {
		t.Fatal("normalization lost Unicode cap", norm)
	}
	if got := goai.FormatProviderError(norm, "Provider API error"); got != "Provider API error (403): "+want {
		t.Fatal("format changed", got)
	}
}

func TestErrorTruncationUTF16ReferenceMain(t *testing.T) {
	alphabet := []rune{'a', 'é', '界', '😀', '\u0301', '𐐀'}
	for word := 0; word < 1296; word++ {
		n := word
		chars := make([]rune, 4)
		for i := range chars {
			chars[i] = alphabet[n%len(alphabet)]
			n /= len(alphabet)
		}
		text := string(chars)
		units := utf16.Encode(chars)
		for limit := 0; limit <= 10; limit++ {
			want := text
			if len(units) > limit {
				end := limit
				if end > 0 && units[end-1] >= 0xd800 && units[end-1] <= 0xdbff {
					end--
				}
				want = string(utf16.Decode(units[:end])) + fmt.Sprintf("... [truncated %d chars]", len(units)-end)
			}
			got := goai.TruncateErrorText(text, limit)
			if got != want || !utf8.ValidString(got) {
				t.Fatalf("reference mismatch input=%q limit=%d got=%q want=%q", text, limit, got, want)
			}
		}
	}
}
