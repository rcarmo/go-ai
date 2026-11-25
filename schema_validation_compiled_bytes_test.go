package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:83–95. These calls pass typed Go bytes, not a JSON
// array with the same numbers or a base64-encoded string.
func TestSourceV160SchemaDeclaredBytes(t *testing.T) {
	const schema = `{"type":"object","properties":{"data":{"type":"bytes","maxLength":4}}}`
	compiled := compileV160LeafSchema(t, schema)
	for _, tc := range []struct {
		name string
		data any
		want bool
	}{
		{"L85-four-bytes", make([]byte, 4), true},
		{"L86-five-bytes", make([]byte, 5), false},
		{"L87-number-array", []any{1, 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160ByteCheck(t, compiled, map[string]any{"data": tc.data}, tc.want)
		})
	}
	t.Run("L88-max-length-diagnostic", func(t *testing.T) {
		issues, err := compiled.Explain(map[string]any{"data": make([]byte, 5)})
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/data","keyword":"maxLength","message":"expected at most 4 bytes"}]`)
	})
	t.Run("L94-bytes-are-not-object", func(t *testing.T) {
		assertV160ByteCheck(t, compileV160LeafSchema(t, `{"type":"object"}`), []byte{0}, false)
	})
}

// The two typed fixtures at validator.test.ts:325–326 complete the L345
// comparison alongside the 64 JSON cases in TestSourceV160SchemaCompiledJSONLeaves.
func TestSourceV160SchemaCompiledByteLeaves(t *testing.T) {
	for _, tc := range []struct {
		name, leaf string
		want       bool
	}{
		{"L325-number-or-array", `{"type":["number","array"]}`, false},
		{"L326-object-or-bytes", `{"type":["object","bytes"]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bytes := []byte{0}
			schemas := [4]string{
				tc.leaf,
				`{"type":"object","properties":{"p":` + tc.leaf + `},"additionalProperties":false}`,
				`{"type":"object","properties":{"p":` + tc.leaf + `},"required":["q"]}`,
				`{"type":"array","items":` + tc.leaf + `}`,
			}
			values := [4]any{bytes, map[string]any{"p": bytes}, map[string]any{"p": bytes, "q": 1}, []any{bytes}}
			var got [4]bool
			for i, schema := range schemas {
				verdict, err := compileV160LeafSchema(t, schema).Check(values[i])
				if err != nil {
					t.Fatal(err)
				}
				got[i] = verdict
			}
			want := tc.want
			if got != [4]bool{want, want, want, want} {
				t.Fatalf("byte leaf [standalone closed open array] = %v; want four %t", got, want)
			}
		})
	}
}

// Supplemental boundaries from the pinned predicate and public JSON decode.
func TestSchemaTypedBytesBoundaries(t *testing.T) {
	const schema = `{"type":"bytes","minLength":2,"maxLength":4}`
	compiled := compileV160LeafSchema(t, schema)
	for _, tc := range []struct {
		name   string
		value  any
		want   bool
		issues string
	}{
		{"below-minimum", make([]byte, 1), false, `[{"path":"","keyword":"minLength","message":"expected at least 2 bytes"}]`},
		{"minimum", make([]byte, 2), true, `[]`},
		{"maximum", make([]byte, 4), true, `[]`},
		{"above-maximum", make([]byte, 5), false, `[{"path":"","keyword":"maxLength","message":"expected at most 4 bytes"}]`},
		{"base64-string", json.RawMessage(`"AAA="`), false, `[{"path":"","keyword":"type","message":"expected bytes, got string"}]`},
		{"json-array", json.RawMessage(`[0,0]`), false, `[{"path":"","keyword":"type","message":"expected bytes, got array"}]`},
		{"json-object", json.RawMessage(`{"0":0,"1":0}`), false, `[{"path":"","keyword":"type","message":"expected bytes, got object"}]`},
		{"json-null", json.RawMessage(`null`), false, `[{"path":"","keyword":"type","message":"expected bytes, got null"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160ByteCheck(t, compiled, tc.value, tc.want)
			got, err := goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), tc.value, goai.JSONSchemaCheckOptions{})
			if err != nil || got != tc.want {
				t.Errorf("standalone check(%T %#v) = %t, %v; want %t", tc.value, tc.value, got, err, tc.want)
			}
			issues, err := compiled.Explain(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			assertV160StructuredDiagnostics(t, issues, tc.issues)
		})
	}
	object := compileV160LeafSchema(t, `{"type":"object"}`)
	issues, err := object.Explain([]byte{0})
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"type","message":"expected object, got bytes"}]`)
}

func assertV160ByteCheck(t *testing.T, compiled *goai.CompiledJSONSchema, value any, want bool) {
	t.Helper()
	got, err := compiled.Check(value)
	if err != nil || got != want {
		t.Errorf("check(%T %#v) = %t, %v; want %t", value, value, got, err, want)
	}
}
