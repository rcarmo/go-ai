package durable

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCodemode120HeadTailUnicodeAndStrictBudget(t *testing.T) {
	text := "START" + strings.Repeat("😀middle", 100) + "FINISH"
	for _, tokens := range []int{0, 1, 5, 10, 20, 100} {
		out := projectCodemodeOutput(text, tokens)
		if !utf8.ValidString(out) || codemodeTextUnits(out) > tokens*4 {
			t.Fatal("projection budget", tokens, out)
		}
		if tokens >= 10 && (!strings.HasPrefix(out, "START") || !strings.HasSuffix(out, "FINISH") || !strings.Contains(out, "truncated")) {
			t.Fatal("head/tail missing", tokens, out)
		}
	}
	if projectCodemodeOutput("short", 100) != "short" {
		t.Fatal("short changed")
	}
	withPath := codemodeOutputWithPath(text, "/owned/output.txt", 30)
	if !strings.Contains(withPath, "/owned/output.txt") || codemodeTextUnits(withPath) > 120 {
		t.Fatal(withPath)
	}
}
func FuzzCodemode120OutputProjection(f *testing.F) {
	f.Add("😀head-middle-tail", 10)
	f.Add("ascii", 0)
	f.Fuzz(func(t *testing.T, text string, tokens int) {
		if !utf8.ValidString(text) || tokens < 0 || tokens > 1000 {
			return
		}
		out := projectCodemodeOutput(text, tokens)
		if !utf8.ValidString(out) || codemodeTextUnits(out) > tokens*4 {
			t.Fatal("invalid bounded projection")
		}
	})
}

func TestCodemode120TokenUnitEstimateSaturatesWithoutOverflow(t *testing.T) {
	max := int(^uint(0) >> 1)
	for _, tokens := range []int{0, 1, 10, max / 4, max/4 + 1, max} {
		units := codemodeTokenUnits(tokens)
		if units < 0 || tokens > 0 && units < tokens {
			t.Fatal(tokens, units)
		}
	}
	text := "START 😀 FINISH"
	if got := boundCodemodeOutput(text, max); got != text {
		t.Fatal(got)
	}
	if got := projectCodemodeOutput(text, max); got != text {
		t.Fatal(got)
	}
	if got := codemodeOutputWithPath(text, "/owned/output.txt", max); !strings.Contains(got, text) || !strings.Contains(got, "/owned/output.txt") {
		t.Fatal(got)
	}
}
