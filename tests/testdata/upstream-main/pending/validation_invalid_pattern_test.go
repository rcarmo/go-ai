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
		// ValidateToolArguments uses the default 2020-12 compiler even when
		// the schema carries a draft-07 URI; the URI alone is not an option.
		{"draft-uri-ref-sibling", `{"$schema":"http://json-schema.org/draft-07/schema#","$defs":{"x":{"type":"number"}},"$ref":"#/$defs/x","pattern":"["}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"draft-uri-dependent-schemas", `{"$schema":"http://json-schema.org/draft-07/schema#","dependentSchemas":{"x":{"type":"string","pattern":"["}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"recursive-reference", `{"$defs":{"x":{"properties":{"next":{"$ref":"#/$defs/x"},"value":{"type":"string","pattern":"["}}}},"$ref":"#/$defs/x"}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"then-with-if", `{"if":{"type":"number"},"then":{"type":"string","pattern":"["}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"then-without-if", `{"then":{"type":"string","pattern":"["}}`, `ok {}`},
		{"else-without-if", `{"else":{"type":"string","pattern":"["}}`, `ok {}`},
		{"content-schema-annotation", `{"contentSchema":{"type":"string","pattern":"["}}`, `ok {}`},
		{"additional-items-default-draft", `{"additionalItems":{"type":"string","pattern":"["}}`, `ok {}`},
		{"bytes-skips-string-group", `{"properties":{"x":{"type":"bytes","pattern":"["}}}`, `ok {}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mainNumericOutcome(t, tc.schema, `{}`); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
