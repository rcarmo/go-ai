package goai_test

import (
	"strings"
	"testing"
)

// Pinned validation-probes.test.ts pattern-unicode-prop assertions.
func TestMainValidationStringPatternUnicodeProperty(t *testing.T) {
	schema := `{"type":"object","properties":{"s":{"type":"string","pattern":"^\\p{L}+$"}}}`
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"s":"äb"}`, true},
		{`{"s":"p{L}"}`, false},
	} {
		got := mainNumericOutcome(t, schema, tc.input)
		if strings.HasPrefix(got, "ok ") != tc.valid {
			t.Errorf("input=%s got=%s valid=%v", tc.input, got, tc.valid)
		}
	}
}
