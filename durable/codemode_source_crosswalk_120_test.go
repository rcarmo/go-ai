package durable

import (
	"strings"
	"testing"
)

// Cases transcribed from pinned packages/codemode/test/source.test.ts.
func TestCodemode120PinnedSourceCases(t *testing.T) {
	for _, code := range []string{"text('hi')", "// just a comment\nreturn 1", "// @optionsx {}\ntext(1)", `text("// @options: {}")`} {
		got, err := ParseCodemodeSource(code)
		if err != nil || got.Code != code {
			t.Fatal(code, got, err)
		}
	}
	for _, tc := range []struct {
		input, code string
		timeout     int64
		tokens      int
	}{{"// @options: {\"timeout_ms\": 10}\nconst a = 1;\ntext(a)", "\nconst a = 1;\ntext(a)", 10, 10000}, {"  // @options:{\"max_output_tokens\":0,\"timeout_ms\":1500}\r\ntext(1)", "\ntext(1)", 1500, 0}, {"// @options: {}\ntext(1)", "\ntext(1)", 300000, 10000}} {
		got, err := ParseCodemodeSource(tc.input)
		if err != nil || got.Code != tc.code || got.TimeoutMs != tc.timeout || got.MaxOutputTokens != tc.tokens {
			t.Fatal(tc, got, err)
		}
	}
	for _, tc := range []struct{ input, line string }{{"// count things\n// @options: {\"timeout_ms\":1000}\nwhile(true){}", "2"}, {"\n  // @options: {\"timeout_ms\":1000}\nwhile(true){}", "2"}, {"text(1)\r\n// @options: {\"timeout_ms\":1}", "2"}, {"// @options: {\"timeout_ms\":1}\ntext(1)\n// @options: {\"timeout_ms\":2}", "3"}} {
		_, err := ParseCodemodeSource(tc.input)
		if err == nil || !strings.Contains(err.Error(), "found one on line "+tc.line) {
			t.Fatal(tc, err)
		}
	}
	for _, input := range []string{"", "  \n", "// @options:\ntext(1)", "// @options: {timeout_ms:1}\ntext(1)", "// @options: [1]\ntext(1)", "// @options: {\"yield\":1}\ntext(1)", "// @options: {\"max_output_tokens\":1.5}\ntext(1)", "// @options: {\"timeout_ms\":0}\ntext(1)", "// @options: {\"timeout_ms\":1}", "// @options: {\"timeout_ms\":1}\n  \n"} {
		if _, err := ParseCodemodeSource(input); err == nil {
			t.Fatal("invalid pinned case admitted", input)
		}
	}
}
