package durable

import (
	"errors"
	"reflect"
	"regexp"
	"testing"
)

// Exact native translation of packages/codemode/test/source.test.ts at
// fb0fe823539f6d04850f6e34748c01f01f212e1f (blob
// 36e7c0b7297dca32e6300e66b4be9abc51d5c10c). The Go result resolves omitted
// options to defaults, where the original observes absent option fields.
func TestSourceV170CodemodeOriginal(t *testing.T) {
	t.Run("returns plain code unchanged", func(t *testing.T) {
		for _, code := range []string{"text('hi')", "// just a comment\nreturn 1"} {
			got, err := ParseCodemodeSource(code)
			if err != nil || got.Code != code || got.TimeoutMs != defaultCodemodeTimeoutMs || got.MaxOutputTokens != defaultCodemodeOutputTokens {
				t.Fatalf("ParseCodemodeSource(%q) = %+v, %v", code, got, err)
			}
		}
	})

	t.Run("parses the options line and keeps line numbers", func(t *testing.T) {
		for _, tc := range []struct {
			input, code string
			timeout     int64
			tokens      int
		}{
			{"// @options: {\"timeout_ms\": 10}\nconst a = 1;\ntext(a)", "\nconst a = 1;\ntext(a)", 10, defaultCodemodeOutputTokens},
			{"  // @options:{\"max_output_tokens\":0,\"timeout_ms\":1500}\r\ntext(1)", "\ntext(1)", 1500, 0},
			{"// @options: {}\ntext(1)", "\ntext(1)", defaultCodemodeTimeoutMs, defaultCodemodeOutputTokens},
		} {
			got, err := ParseCodemodeSource(tc.input)
			if err != nil || got.Code != tc.code || got.TimeoutMs != tc.timeout || got.MaxOutputTokens != tc.tokens {
				t.Fatalf("ParseCodemodeSource(%q) = %+v, %v; want code %q timeout %d tokens %d", tc.input, got, err, tc.code, tc.timeout, tc.tokens)
			}
		}
	})

	t.Run("only treats the first line as an options line", func(t *testing.T) {
		for _, code := range []string{"// @optionsx {}\ntext(1)", `text("// @options: {}")`} {
			got, err := ParseCodemodeSource(code)
			if err != nil || got.Code != code {
				t.Fatalf("ParseCodemodeSource(%q) = %+v, %v", code, got, err)
			}
		}
	})

	t.Run("rejects an options line that is not the first line", func(t *testing.T) {
		for _, tc := range []struct{ input, message string }{
			{"// count things\n// @options: {\"timeout_ms\": 1000}\nwhile (true) {}", "The // @options: line must be the first line of the input; found one on line 2"},
			{"\n  // @options: {\"timeout_ms\": 1000}\nwhile (true) {}", "The // @options: line must be the first line of the input; found one on line 2"},
			{"text(1)\r\n// @options: {\"timeout_ms\": 1}", "The // @options: line must be the first line of the input; found one on line 2"},
			{"// @options: {\"timeout_ms\": 1}\ntext(1)\n// @options: {\"timeout_ms\": 2}", "The // @options: line must be the first line of the input; found one on line 3"},
		} {
			_, err := ParseCodemodeSource(tc.input)
			assertSourceV170Error(t, err, "^"+regexp.QuoteMeta(tc.message)+"$")
		}
	})

	t.Run("rejects empty input and invalid options", func(t *testing.T) {
		for _, tc := range []struct{ input, pattern string }{
			{"", `Expected JavaScript source text \(non-empty\)`},
			{"  \n", `Expected JavaScript source text \(non-empty\)`},
			{"// @options:\ntext(1)", `@options must be a JSON object with supported fields`},
			{"// @options: {timeout_ms:1}\ntext(1)", `@options must be valid JSON with supported fields`},
			{"// @options: [1]\ntext(1)", `@options must be a JSON object with supported fields`},
			{"// @options: {\"yield\":1}\ntext(1)", "@options only supports `max_output_tokens` and `timeout_ms`; got `yield`"},
			{"// @options: {\"max_output_tokens\":1.5}\ntext(1)", `@options field ` + "`max_output_tokens`" + ` must be a non-negative safe integer`},
			{"// @options: {\"timeout_ms\":0}\ntext(1)", `@options field ` + "`timeout_ms`" + ` must be a positive integer`},
			{"// @options: {\"timeout_ms\":1}", `The @options line must be followed by JavaScript source on subsequent lines`},
			{"// @options: {\"timeout_ms\":1}\n  \n", `The @options line must be followed by JavaScript source on subsequent lines`},
		} {
			_, err := ParseCodemodeSource(tc.input)
			assertSourceV170Error(t, err, tc.pattern)
		}
	})
}

func assertSourceV170Error(t *testing.T, err error, pattern string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected CodemodeSourceError, got nil")
	}
	typeOf := reflect.TypeOf(err)
	if typeOf.Kind() != reflect.Pointer || typeOf.Elem().Name() != "CodemodeSourceError" {
		t.Fatalf("expected *CodemodeSourceError, got %T: %v", err, err)
	}
	var sourceError *CodemodeSourceError
	if !errors.As(err, &sourceError) {
		t.Fatalf("errors.As did not find *CodemodeSourceError in %T", err)
	}
	if !regexp.MustCompile(pattern).MatchString(err.Error()) {
		t.Fatalf("CodemodeSourceError %q does not match %q", err, pattern)
	}
}
