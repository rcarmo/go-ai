package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// equal.test.ts:8–28. Both pinned implementations produced this ten-entry
// vector. The typed sentinel preserves the two non-JSON undefined inputs.
func TestSchemaEqualityOriginalCases(t *testing.T) {
	undefined := goai.SchemaUndefined{}
	cases := [10][2]any{
		{float64(1), float64(1)},
		{"a", "b"},
		{nil, nil},
		{nil, undefined},
		// These JSON inputs list identical object members in opposite order.
		{
			decodeSchemaEqualityJSON(t, `{"a":1,"b":[1,{"c":2}]}`),
			decodeSchemaEqualityJSON(t, `{"b":[1,{"c":2}],"a":1}`),
		},
		{map[string]any{"a": float64(1)}, map[string]any{"a": float64(1), "b": float64(2)}},
		{[]any{float64(1), float64(2)}, []any{float64(2), float64(1)}},
		{[]any{float64(1)}, map[string]any{"0": float64(1)}},
		{map[string]any{}, []any{}},
		{map[string]any{"a": undefined}, map[string]any{}},
	}
	want := [10]bool{true, false, true, false, true, false, false, false, false, false}
	var got, reversed [10]bool
	for i, pair := range cases {
		got[i] = goai.SchemaEqual(pair[0], pair[1])
		reversed[i] = goai.SchemaEqual(pair[1], pair[0])
	}
	if got != want {
		t.Fatalf("equality in source order = %v; want %v", got, want)
	}
	if reversed != want {
		t.Fatalf("equality with operands reversed = %v; want %v", reversed, want)
	}

	for i, pair := range cases {
		for _, keyword := range []string{"const", "enum"} {
			value := pair[1]
			if keyword == "enum" {
				value = []any{pair[1]}
			}
			schema, err := json.Marshal(map[string]any{keyword: value})
			if err != nil {
				// Explicit undefined is a native-only input and cannot be a JSON schema literal.
				continue
			}
			compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{})
			if err != nil {
				t.Fatalf("%s case %d compile: %v", keyword, i, err)
			}
			accepted, err := compiled.Check(pair[0])
			if err != nil || accepted != want[i] {
				t.Errorf("%s case %d: accepted = %v, error = %v; want %v", keyword, i, accepted, err, want[i])
			}
		}
	}

	compiled, err := goai.CompileJSONSchema([]byte(`{"uniqueItems":true}`), goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i, pair := range cases {
		accepted, err := compiled.Check([]any{pair[0], pair[1]})
		if err != nil {
			t.Fatalf("uniqueItems case %d: %v", i, err)
		}
		if accepted != !want[i] {
			t.Errorf("uniqueItems case %d: accepted = %v; want %v", i, accepted, !want[i])
		}
	}
	cyclic := map[string]any{"undefined": undefined}
	cyclic["self"] = cyclic
	if accepted, err := compiled.Check([]any{cyclic}); err != nil || accepted {
		t.Fatalf("cyclic unique item: accepted = %v, error = %v; want false", accepted, err)
	}
}

func decodeSchemaEqualityJSON(t *testing.T, raw string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
