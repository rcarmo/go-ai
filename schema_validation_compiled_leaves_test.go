package goai_test

import (
	"encoding/json"
	"fmt"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:345, restricted to the JSON values of L320–334. The
// Uint8Array inputs at L325–326 are separate JS values, not JSON arrays or
// objects. The pinned standalone and embedded checks all produced the listed
// verdicts; agreement alone would not detect a shared bug.
func TestSourceV160SchemaCompiledJSONLeaves(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
		values       []string
		want         []bool
	}{
		{"L320-string-const", `{"const":"a"}`,
			[]string{`"a"`, `"b"`, `1`, `null`}, []bool{true, false, false, false}},
		{"L321-null-const", `{"const":null}`,
			[]string{`null`, `0`, `false`, `"null"`}, []bool{true, false, false, false}},
		{"L322-mixed-enum", `{"enum":["a",1,null]}`,
			[]string{`"a"`, `1`, `null`, `"b"`, `2`, `true`}, []bool{true, true, true, false, false, false}},
		{"L323-string-or-null", `{"type":["string","null"]}`,
			[]string{`"a"`, `null`, `1`, `{}`, `[]`}, []bool{true, true, false, false, false}},
		{"L324-integer-or-boolean", `{"type":["integer","boolean"]}`,
			[]string{`1`, `1.5`, `true`, `"1"`}, []bool{true, false, true, false}},
		{"L325-number-or-array", `{"type":["number","array"]}`,
			[]string{`1.5`, `2`, `[]`, `{}`}, []bool{true, true, true, false}},
		{"L326-object-or-bytes", `{"type":["object","bytes"]}`,
			[]string{`{}`, `[]`, `null`}, []bool{true, false, false}},
		{"L327-string-or-null-union", `{"anyOf":[{"type":"string"},{"type":"null"}]}`,
			[]string{`"a"`, `null`, `1`}, []bool{true, true, false}},
		{"L328-integer-or-long-string", `{"anyOf":[{"type":"integer"},{"type":"string","minLength":2}]}`,
			[]string{`1`, `1.5`, `"ab"`, `"a"`}, []bool{true, false, true, false}},
		{"L329-integer-bounds", `{"type":"integer","minimum":1,"exclusiveMaximum":5}`,
			[]string{`1`, `4`, `5`, `0`, `2.5`, `"2"`}, []bool{true, true, false, false, false, false}},
		{"L330-number-bounds", `{"type":"number","exclusiveMinimum":0,"maximum":1}`,
			[]string{`0.5`, `1`, `0`, `1.5`, `"0.5"`}, []bool{true, true, false, false, false}},
		{"L331-integer-multiple", `{"type":"integer","multipleOf":2}`,
			[]string{`2`, `3`, `2.5`}, []bool{true, false, false}},
		{"L332-string-length", `{"type":"string","minLength":2,"maxLength":3}`,
			[]string{`"ab"`, `"abc"`, `"a"`, `"abcd"`, `"😀😀"`, `12`}, []bool{true, true, false, false, true, false}},
		{"L333-string-pattern", `{"type":"string","pattern":"^a+$"}`,
			[]string{`"aa"`, `"ab"`, `1`}, []bool{true, false, false}},
		{"L334-untyped-minimum", `{"minimum":1}`,
			[]string{`1`, `0`, `"x"`, `null`}, []bool{true, false, true, true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.values) != len(tc.want) {
				t.Fatal("fixture values and verdicts differ in length")
			}
			schemas := [4]string{
				tc.schema,
				`{"type":"object","properties":{"p":` + tc.schema + `},"additionalProperties":false}`,
				`{"type":"object","properties":{"p":` + tc.schema + `},"required":["q"]}`,
				`{"type":"array","items":` + tc.schema + `}`,
			}
			var validators [4]*goai.CompiledJSONSchema
			for i, schema := range schemas {
				validators[i] = compileV160LeafSchema(t, schema)
			}
			for i, value := range tc.values {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					inputs := [4]string{value, `{"p":` + value + `}`, `{"p":` + value + `,"q":1}`, `[` + value + `]`}
					var got [4]bool
					for i, v := range validators {
						verdict, err := v.Check(json.RawMessage(inputs[i]))
						if err != nil {
							t.Fatalf("check(%s): %v", inputs[i], err)
						}
						got[i] = verdict
					}
					want := tc.want[i]
					if got != [4]bool{want, want, want, want} {
						t.Fatalf("check(%s) [standalone closed open array] = %v; want four %t", value, got, want)
					}
				})
			}
		})
	}
}

// validator.test.ts:354–369. Raw inputs retain the source's key ordering
// before Go decodes them. L367 uses a typed value for the non-JSON case.
func TestSourceV160SchemaCompiledPropertyOrder(t *testing.T) {
	const schema = `{"type":"object","properties":{"a":{"const":1},"b":{"type":"string"},"c":{"type":"integer","minimum":0}},"required":["a","c"],"additionalProperties":false}`
	compiled := compileV160LeafSchema(t, schema)
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		{"L361-declared-order", `{"a":1,"b":"x","c":0}`, true},
		{"L362-reversed-order", `{"c":0,"b":"x","a":1}`, true},
		{"L363-optional-first", `{"b":"x","c":0,"a":1}`, true},
		{"L364-unknown-key", `{"a":1,"c":0,"a2":1}`, false},
		{"L365-bound", `{"c":-1,"a":1}`, false},
		{"L366-wrong-type", `{"c":0,"b":1,"a":1}`, false},
		{"L368-missing-required", `{"c":0,"b":"x"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compiled.Check(json.RawMessage(tc.value))
			if err != nil || got != tc.want {
				t.Fatalf("check(%s) = %t, %v; want %t", tc.value, got, err, tc.want)
			}
		})
	}
	t.Run("L367-optional-key-with-undefined", func(t *testing.T) {
		got, err := compiled.Check(map[string]any{"c": 0, "b": goai.SchemaUndefined{}, "a": 1})
		if err != nil || !got {
			t.Fatalf("check with optional undefined = %t, %v; want true", got, err)
		}
	})
}

func compileV160LeafSchema(t *testing.T, schema string) *goai.CompiledJSONSchema {
	t.Helper()
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}
