package goai_test

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Exact JSON schemas, inputs and result from b556cfe5 packages/ai/test/
// validation-probes.test.ts:735-750, "reviewer probes (round 2)".
// The native Tool schema uses JSON bytes instead of a JavaScript object.
func TestUpstreamMainValidationOptionalNullPropertyValues(t *testing.T) {
	cases := []struct{ name, schema string }{
		{"declared", `{"type":"object","properties":{"entry":{"type":"object","properties":{"n":{"type":"integer","minimum":1}}}}}`},
		{"additional", `{"type":"object","additionalProperties":{"type":"object","properties":{"n":{"type":"integer","minimum":1}}}}`},
		{"pattern", `{"type":"object","patternProperties":{"^entry$":{"type":"object","properties":{"n":{"type":"integer","minimum":1}}}},"additionalProperties":false}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := goai.Tool{Name: tc.name, Parameters: json.RawMessage(tc.schema)}
			call := goai.ToolCall{Name: tc.name, Arguments: map[string]interface{}{"entry": map[string]interface{}{"n": nil}}}
			got, err := goai.ValidateToolArguments(&tool, call)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{"entry": map[string]interface{}{}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("source expects optional null removed: got %#v, want %#v", got, want)
			}
		})
	}
}

// Native supplement to the exact source probes: validation.ts parses a
// structuredClone. Removing nested nulls must not change caller-owned values.
// This checks touched objects/arrays; arbitrary returned-value aliasing is a
// separate, broader structuredClone contract.
func TestUpstreamMainValidationOptionalNullInputOwnership(t *testing.T) {
	for i, schema := range []string{
		`{"type":"object","properties":{"entry":{"type":"object","properties":{"n":{"type":"integer"}}}}}`,
		`{"type":"object","properties":{"entry":{"type":"array","items":{"type":"object","properties":{"n":{"type":"integer"}}}}}}`,
		`{"type":"object","properties":{"entry":{"type":"array","items":[{"type":"object","properties":{"n":{"type":"integer"}}}]}}}`,
	} {
		t.Run([]string{"object", "array", "tuple"}[i], func(t *testing.T) {
			var input map[string]interface{}
			if err := json.Unmarshal([]byte(`{"entry":{"n":null}}`), &input); err != nil {
				t.Fatal(err)
			}
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
				t.Fatal(err)
			}
			entry := parsed["properties"].(map[string]interface{})["entry"].(map[string]interface{})
			if entry["type"] == "array" {
				input["entry"] = []interface{}{input["entry"]}
			}
			before, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			tool := goai.Tool{Name: "ownership", Parameters: json.RawMessage(schema)}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: input})
			if err != nil {
				t.Fatal(err)
			}
			after, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			var beforeValue, afterValue interface{}
			if err := json.Unmarshal(before, &beforeValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(after, &afterValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(beforeValue, afterValue) {
				t.Fatalf("normalisation mutated input: before %s, after %s (result %#v)", before, after, got)
			}
		})
	}
}

// Native regression controls for which schema governs each property. These
// supplement the three translated probes; they are not extra upstream tests.
func TestUpstreamMainValidationOptionalNullBoundaries(t *testing.T) {
	cases := []struct{ name, schema, input, want string }{
		{
			"declared-pattern-and-additional",
			`{"type":"object","properties":{"entry":{"type":"object","properties":{"n":{"type":"integer"}}}},"patternProperties":{"^entry$":{"type":"object","properties":{"m":{"type":"integer"}}}},"additionalProperties":{"type":"object","properties":{"secret":{"type":"integer"}}}}`,
			`{"entry":{"n":null,"m":null,"secret":null},"extra":{"n":null,"secret":null}}`,
			`{"entry":{"secret":null},"extra":{"n":null}}`,
		},
		{
			"nullable-required-and-unmentioned",
			`{"type":"object","properties":{"entry":{"type":"object","properties":{"optional":{"type":"integer"},"nullable":{"type":["integer","null"]},"required":{"type":["integer","null"]}},"required":["required"]}}}`,
			`{"entry":{"optional":null,"nullable":null,"required":null,"unmentioned":null}}`,
			`{"entry":{"nullable":null,"required":null,"unmentioned":null}}`,
		},
		{
			"boolean-pattern-also-excludes-additional-schema",
			`{"type":"object","patternProperties":{"^entry$":true},"additionalProperties":{"type":"object","properties":{"n":{"type":"integer"}}}}`,
			`{"entry":{"n":null},"extra":{"n":null}}`,
			`{"entry":{"n":null},"extra":{}}`,
		},
		{
			"pattern-and-additional-keys-are-not-optional",
			`{"type":"object","patternProperties":{"^match$":{"type":["object","null"]}},"additionalProperties":{"type":["object","null"]}}`,
			`{"match":null,"extra":null}`,
			`{"match":null,"extra":null}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var input, want map[string]interface{}
			if err := json.Unmarshal([]byte(tc.input), &input); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			tool := goai.Tool{Name: tc.name, Parameters: json.RawMessage(tc.schema)}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: input})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %#v, want %#v", got, want)
			}
		})
	}
}
