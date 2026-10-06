package tools

import (
	"testing"

	"golang.org/x/text/unicode/norm"
)

// GO-2026-5970: malformed UTF-8 must advance the normaliser rather than loop.
// The first case is the illegal-rune regression in the fixed x/text release.
func TestUnicodeNormaliserMalformedUTF8Regression(t *testing.T) {
	for _, input := range []string{"\xf3\xcc\x80", "\xff", "\xe2\x82", "\xed\xa0\x80", "\xf4\x90\x80\x80"} {
		for _, form := range []norm.Form{norm.NFD, norm.NFKC} {
			var iter norm.Iter
			iter.InitString(form, input)
			previous := iter.Pos()
			for steps := 0; !iter.Done(); steps++ {
				if steps >= len(input) {
					t.Fatalf("normalisation did not finish for %x", input)
				}
				iter.Next()
				if iter.Pos() <= previous {
					t.Fatalf("normalisation did not advance for %x at %d", input, previous)
				}
				previous = iter.Pos()
			}
			if got := form.String(input); got != input {
				t.Fatalf("malformed bytes changed: got %x want %x", got, input)
			}
		}
		// The fuzzy helper maps runes, so malformed byte sequences become
		// U+FFFD after the fixed normaliser has advanced over them.
		want := string([]rune(input))
		if got := normalizeFuzzy(input); got != want {
			t.Fatalf("fuzzy replacement: got %x want %x", got, want)
		}
	}
}
