package goai_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV160SchemaStandaloneCoerce(t *testing.T) {
	for _, tc := range []struct{ name, schema, input, want string }{
		{"L52-L53-keeps-input", `{"type":"object","properties":{"count":{"type":"number"},"tags":{"type":"array","items":{"type":"string"}}},"required":["count","tags"],"additionalProperties":false}`, `{"count":"42","tags":"a"}`, `{"count":42,"tags":["a"]}`},
		{"L96-L97-leaves-unconvertible", `{"type":"object","properties":{"count":{"type":"number"}},"required":["count"],"additionalProperties":false}`, `{"count":"many"}`, `{"count":"many"}`},
		{"L374-leaves-unconvertible-recursive", v160StandaloneRecursiveTree, `{"t":{"a":1}}`, `{"t":{"a":1}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := decodeV160DiagnosticInput(t, tc.input)
			got, err := goai.CoerceJSONSchemaValueWithResources(json.RawMessage(tc.schema), value, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertV160StandaloneJSON(t, got, tc.want)
			if !reflect.DeepEqual(value, decodeV160DiagnosticInput(t, tc.input)) {
				t.Fatalf("coerce changed the input: %#v", value)
			}
			if tc.name == "L96-L97-leaves-unconvertible" {
				schema := decodeV160DiagnosticInput(t, tc.schema).(map[string]any)
				if goai.CheckJSONSchemaValue(schema, got) {
					t.Fatal("check accepted unconvertible value")
				}
			}
		})
	}
}

const v160StandaloneRecursiveTree = `{
	"type":"object","properties":{"t":{"$ref":"#/$defs/Tree"}},
	"$defs":{"Tree":{"anyOf":[{"type":"string"},{"type":"array","items":{"$ref":"#/$defs/Tree"}}]}}
}`

func TestSourceV160SchemaStandaloneParse(t *testing.T) {
	const read = `{"type":"object","properties":{"count":{"type":"number"},"tags":{"type":"array","items":{"type":"string"}}},"required":["count","tags"],"additionalProperties":false}`
	const entry = `{"type":"object","properties":{"count":{"type":"number"},"name":{"type":"string"}},"required":["count","name"],"additionalProperties":false}`
	for _, tc := range []struct{ name, schema, input, want, errors string }{
		{"L105-valid-after-coercion", read, `{"count":"42","tags":"a"}`, `{"count":42,"tags":["a"]}`, `[]`},
		{"L111-errors-after-coercion", entry, `{"count":"many","name":7}`, `null`, `[{"path":"/count","keyword":"type","message":"expected number, got string"}]`},
		{"validator-L217-relative-root-id", `{"$id":"schemas/tool.json","type":"object","properties":{"count":{"$ref":"#/$defs/count"}},"required":["count"],"$defs":{"count":{"type":"integer"}}}`, `{"count":"2"}`, `{"count":2}`, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := decodeV160DiagnosticInput(t, tc.input)
			got, issues, err := goai.ParseJSONSchemaValueWithResources(json.RawMessage(tc.schema), value, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertV160StandaloneJSON(t, got, tc.want)
			assertV160StructuredDiagnostics(t, issues, tc.errors)
			if !reflect.DeepEqual(value, decodeV160DiagnosticInput(t, tc.input)) {
				t.Fatalf("parse changed the input: %#v", value)
			}
		})
	}
	// coerce.test.ts:L132–137: strict validation still rejects both fields.
	issues, err := goai.ExplainJSONSchemaValueWithResources(json.RawMessage(read), json.RawMessage(`{"count":"42","tags":"a"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, issues, `[{"path":"/count","keyword":"type","message":"expected number, got string"},{"path":"/tags","keyword":"type","message":"expected array, got string"}]`)
}

// coerce.test.ts:L246–258: the first operation resolves the relative remote
// from the root ID even though the compiler also sees a dynamic reference.
func TestSourceV160SchemaStandaloneRelativeReference(t *testing.T) {
	schema := json.RawMessage(`{
		"$id":"https://example.com/root.json","$dynamicAnchor":"node","type":"object",
		"properties":{"count":{"$ref":"defs/n.json"},"children":{"type":"array","items":{"$dynamicRef":"#node"}}}
	}`)
	resources := map[string]json.RawMessage{"https://example.com/defs/n.json": json.RawMessage(`{"type":"integer"}`)}
	value := decodeV160DiagnosticInput(t, `{"count":"2"}`)
	t.Run("coerce", func(t *testing.T) {
		got, err := goai.CoerceJSONSchemaValueWithResources(schema, value, resources)
		if err != nil {
			t.Fatal(err)
		}
		assertV160StandaloneJSON(t, got, `{"count":2}`)
	})
	t.Run("parse", func(t *testing.T) {
		for range 2 {
			got, issues, err := goai.ParseJSONSchemaValueWithResources(schema, value, resources)
			if err != nil {
				t.Fatal(err)
			}
			assertV160StandaloneJSON(t, got, `{"count":2}`)
			assertV160StructuredDiagnostics(t, issues, `[]`)
		}
		assertV160StandaloneJSON(t, value, `{"count":"2"}`)
	})
}

// coerce.test.ts:L414–437: keep all 41 required errors and the leaf type error.
func TestSourceV160SchemaStandaloneRecursiveParse(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object","properties":{"s":{"$ref":"#/$defs/S"}},
		"$defs":{"S":{"anyOf":[
			{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["a"]},
			{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["b"]}
		]}}
	}`)
	value := any(map[string]any{"x": 1})
	want := make([]goai.JSONSchemaDiagnostic, 0, 42)
	for i := range 41 {
		want = append(want, goai.JSONSchemaDiagnostic{Path: "/s" + strings.Repeat("/c", i) + "/a", Keyword: "required", Message: "missing"})
		if i < 40 {
			value = map[string]any{"c": value}
		}
	}
	want = append(want, goai.JSONSchemaDiagnostic{Path: "/s" + strings.Repeat("/c", 40) + "/x", Keyword: "type", Message: "expected object, got integer"})
	got, issues, err := goai.ParseJSONSchemaValueWithResources(schema, map[string]any{"s": value}, nil)
	if got != nil || err != nil || !reflect.DeepEqual(issues, want) {
		t.Fatalf("parse = %#v, %+v, %v; want nil and %+v", got, issues, err, want)
	}
}

// Supplemental: a valid null and a diagnostic rejection both return a nil
// value, but only the rejection returns a record. Setup failures are errors.
func TestSchemaStandaloneParseResultBoundaries(t *testing.T) {
	value, issues, err := goai.ParseJSONSchemaValueWithResources(json.RawMessage(`{"type":"null"}`), json.RawMessage(`"null"`), nil)
	if value != nil || err != nil {
		t.Fatalf("null parse = %#v, %+v, %v; want successful null", value, issues, err)
	}
	assertV160StructuredDiagnostics(t, issues, `[]`)

	value, issues, err = goai.ParseJSONSchemaValueWithResources(json.RawMessage(`false`), json.RawMessage(`1`), nil)
	if value != nil || err != nil {
		t.Fatalf("false schema parse = %#v, %+v, %v; want structured rejection", value, issues, err)
	}
	assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"false","message":"no value is allowed here"}]`)

	const uri = "https://example.com/invalid.json"
	value, issues, err = goai.ParseJSONSchemaValueWithResources(json.RawMessage(`true`), 1, map[string]json.RawMessage{uri: json.RawMessage(`42`)})
	if value != nil || issues != nil || err == nil || err.Error() != `schema resource "https://example.com/invalid.json": expected an object or boolean` {
		t.Fatalf("invalid setup = %#v, %+v, %v; want setup error", value, issues, err)
	}
}

func assertV160StandaloneJSON(t *testing.T, got any, want string) {
	t.Helper()
	encoded, err := json.Marshal(got)
	if err != nil || string(encoded) != want {
		t.Fatalf("value = %s, %v; want %s", encoded, err, want)
	}
}
