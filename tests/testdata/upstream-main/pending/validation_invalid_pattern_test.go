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

// The fallback anchor is compiled eagerly, but an overriding anchor in the
// dynamic scope is compiled only when validation reaches the dynamic reference.
func TestMainValidationInvalidPatternDynamicScopeLazy(t *testing.T) {
	schema := `{"$id":"https://example.test/outer","$defs":{"bad":{"$dynamicAnchor":"node","type":"string","pattern":"["},"inner":{"$id":"https://example.test/inner","$dynamicAnchor":"node","properties":{"child":{"$dynamicRef":"#node"}}}},"properties":{"x":{"$ref":"https://example.test/inner"}}}`
	for _, tc := range []struct{ input, want string }{
		{`{}`, `ok {}`},
		{`{"x":{}}`, `ok {"x":{}}`},
		{`{"x":{"child":"abc"}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := mainNumericOutcome(t, schema, tc.input); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
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
		{"dynamic-pointer", `{"$defs":{"x":{"type":"string","pattern":"["}},"properties":{"x":{"$dynamicRef":"#/$defs/x"}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"dynamic-anchor", `{"$defs":{"x":{"$dynamicAnchor":"node","type":"string","pattern":"["}},"properties":{"x":{"$dynamicRef":"#node"}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"unused-dynamic-anchor", `{"$defs":{"x":{"$dynamicAnchor":"node","type":"string","pattern":"["}},"properties":{"x":{"type":"string"}}}`, `ok {}`},
		{"dynamic-scope-unused-anchor", `{"$dynamicAnchor":"node","$defs":{"x":{"$dynamicAnchor":"other","type":"string","pattern":"["}},"properties":{"x":{"$dynamicRef":"#node"}}}`, `ok {}`},
		{"union-const-pattern", `{"properties":{"x":{"anyOf":[{"const":1,"pattern":"["},{"const":2}]}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"union-const-annotation", `{"properties":{"x":{"anyOf":[{"const":1,"default":{"pattern":"["}},{"const":2}]}}}`, `ok {}`},
		{"tagged-union", `{"properties":{"x":{"anyOf":[{"type":"object","properties":{"kind":{"const":"a"},"x":{"type":"string","pattern":"["}},"required":["kind"]},{"type":"object","properties":{"kind":{"const":"b"}},"required":["kind"]}]}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"tagged-union-refs", `{"$defs":{"a":{"type":"object","properties":{"kind":{"const":"a"},"x":{"type":"string","pattern":"["}},"required":["kind"]},"b":{"type":"object","properties":{"kind":{"const":"b"}},"required":["kind"]}},"properties":{"x":{"anyOf":[{"$ref":"#/$defs/a"},{"$ref":"#/$defs/b"}]}}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"then-with-if", `{"if":{"type":"number"},"then":{"type":"string","pattern":"["}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		// With unevaluated keywords, the pinned compiler takes its separate
		// annotating path, which compiles an if even without then/else.
		{"if-alone-plain", `{"if":{"type":"string","pattern":"["}}`, `ok {}`},
		{"if-alone-annotating", `{"if":{"type":"string","pattern":"["},"unevaluatedProperties":true}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"then-alone-annotating", `{"then":{"type":"string","pattern":"["},"unevaluatedProperties":true}`, `ok {}`},
		{"else-alone-annotating", `{"else":{"type":"string","pattern":"["},"unevaluatedItems":true}`, `ok {}`},
		{"unevaluated-properties", `{"unevaluatedProperties":{"type":"string","pattern":"["}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"unevaluated-items", `{"unevaluatedItems":{"type":"string","pattern":"["}}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"dependencies-annotating", `{"dependencies":{"x":{"type":"string","pattern":"["}},"unevaluatedProperties":true}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"dependent-schemas-annotating", `{"dependentSchemas":{"x":{"type":"string","pattern":"["}},"unevaluatedProperties":true}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"additional-items-annotating-no-tuple", `{"additionalItems":{"type":"string","pattern":"["},"unevaluatedItems":true}`, `ok {}`},
		{"additional-items-annotating-tuple", `{"items":[true],"additionalItems":{"type":"string","pattern":"["},"unevaluatedItems":true}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
		{"bytes-pattern-annotating", `{"properties":{"x":{"type":"bytes","pattern":"["}},"unevaluatedProperties":true}`, `ok {}`},
		{"pattern-key-annotating", `{"patternProperties":{"[":true},"unevaluatedProperties":true}`, `THROW Invalid regular expression: /[/: Unterminated character class`},
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
