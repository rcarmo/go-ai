package goai_test

import (
	"testing"
)

// Exact upstream mcp-invalid-regex outcome at v1.5.0.
func TestMainValidationInvalidPatternCompilation(t *testing.T) {
	schema := `{"type":"object","properties":{"s":{"type":"string","pattern":"["}}}`
	want := "THROW Invalid regular expression: /[/: Unterminated character class"
	for _, input := range []string{`{"s":"y"}`, `{}`, `{"s":4}`} {
		if got := mainNumericOutcome(t, schema, input); got != want {
			t.Errorf("input=%s got %q want %q", input, got, want)
		}
	}
}

// Source-derived from the pinned compiler: unused definitions are not checks,
// while referenced definitions and declared properties compile regardless of data.
func TestMainValidationInvalidPatternReachability(t *testing.T) {
	for _, tc := range []struct{ name, schema, want string }{
		{"unused-definition", `{"type":"object","$defs":{"x":{"type":"string","pattern":"["}}}`, `ok {}`},
		{"annotation-is-data", `{"type":"object","default":{"pattern":"["}}`, `ok {}`},
		{"referenced-definition", `{"type":"object","$defs":{"x":{"type":"string","pattern":"["}},"properties":{"s":{"$ref":"#/$defs/x"}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"pattern-key", `{"type":"object","patternProperties":{"[":true}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"array-item", `{"type":"object","properties":{"a":{"type":"array","items":{"type":"string","pattern":"["}}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"union-branch", `{"type":"object","properties":{"s":{"anyOf":[{"type":"string","pattern":"["},{"type":"number"}]}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"ref-sibling", `{"type":"object","$defs":{"x":{"type":"number"}},"properties":{"s":{"$ref":"#/$defs/x","pattern":"["}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mainNumericOutcome(t, tc.schema, `{}`); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
