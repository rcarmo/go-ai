package goai_test

import (
	"strings"
	"testing"
)

// Source-derived from sharedRegex in pinned compiler.ts; RE2 lacks lookbehind.
// The value is wrapped in a required native tool argument.
func TestMainValidationPatternPropertiesLookbehind(t *testing.T) {
	schema := `{"type":"object","properties":{"v":{"type":"object","patternProperties":{"(?<!x)y":{"type":"integer"}},"additionalProperties":false}},"required":["v"]}`
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"v":{"y":1}}`, true},
		{`{"v":{"y":"wrong"}}`, false},
		{`{"v":{"xy":1}}`, false},
	} {
		got := mainNumericOutcome(t, schema, tc.input)
		if strings.HasPrefix(got, "ok ") != tc.valid {
			t.Errorf("input=%s got=%q valid=%v", tc.input, got, tc.valid)
		}
	}
}
