package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:503–520. The three TypeBox choices serialize to the
// union and the two unconstrained member schemas shown below.
func TestSourceV160SchemaUndefinedPropertyPresence(t *testing.T) {
	undefined := goai.SchemaUndefined{}
	for _, tc := range []struct {
		name, property string
		nullAccepted   bool
	}{
		{"union", `{"anyOf":[{"type":"string"},{"type":"undefined"}]}`, false},
		{"unknown", `{}`, true},
		{"any", `{}`, true},
	} {
		for _, mode := range []struct{ name, extra string }{{"open", ""}, {"closed", `,"additionalProperties":false`}} {
			t.Run(tc.name+"/"+mode.name, func(t *testing.T) {
				schema := `{"type":"object","required":["a"],"properties":{"a":` + tc.property + `}` + mode.extra + `}`
				compiled := compileV160LeafSchema(t, schema)
				for _, value := range []struct {
					name  string
					input any
					want  bool
				}{
					{"L510-present-undefined", map[string]any{"a": undefined}, true},
					{"L511-missing-key", map[string]any{}, false},
					{"supplemental-json-null", json.RawMessage(`{"a":null}`), tc.nullAccepted},
				} {
					t.Run(value.name, func(t *testing.T) {
						got, err := compiled.Check(value.input)
						if err != nil || got != value.want {
							t.Fatalf("check(%#v) = %t, %v; want %t", value.input, got, err, value.want)
						}
					})
				}
			})
		}
	}
	const required = `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`
	const optional = `{"type":"object","properties":{"name":{"type":"string"}}}`
	input := map[string]any{"name": undefined}
	t.Run("L516-required-string-rejects-undefined", func(t *testing.T) {
		assertV160UndefinedCheck(t, required, input, false)
	})
	t.Run("L517-required-undefined-reports-missing", func(t *testing.T) {
		issues, err := compileV160LeafSchema(t, required).Explain(input)
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/name","keyword":"required","message":"missing"}]`)
	})
	t.Run("L519-optional-undefined-counts-as-absent", func(t *testing.T) {
		assertV160UndefinedCheck(t, optional, input, true)
		assertV160UndefinedCheck(t, optional, json.RawMessage(`{"name":null}`), false)
		if _, present := input["name"].(goai.SchemaUndefined); !present {
			t.Fatal("check removed or rewrote the caller's key")
		}
	})
}

// Supplemental results from the unchanged pinned compiler: omission for an
// optional declared property does not suppress overlapping or extra rules.
func TestSchemaUndefinedStillChecksOtherObjectRules(t *testing.T) {
	for _, tc := range []struct{ name, schema, key, diagnostics string }{
		{"overlapping-pattern", `{"type":"object","properties":{"a":{"type":"string"}},"patternProperties":{"^a$":{"type":"number"}}}`, "a", `[{"path":"/a","keyword":"type","message":"expected number, got undefined"}]`},
		{"unknown-key", `{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":false}`, "extra", `[{"path":"/extra","keyword":"additionalProperties","message":"unknown field"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]any{tc.key: goai.SchemaUndefined{}}
			assertV160UndefinedCheck(t, tc.schema, input, false)
			issues, err := compileV160LeafSchema(t, tc.schema).Explain(input)
			if err != nil {
				t.Fatal(err)
			}
			assertV160StructuredDiagnostics(t, issues, tc.diagnostics)
		})
	}
}

func TestSchemaUndefinedLocalAnnotationStillChecksValue(t *testing.T) {
	const schema = `{"type":"object","properties":{"a":{"type":"string"}},"unevaluatedProperties":false}`
	assertV160UndefinedCheck(t, schema, map[string]any{"a": goai.SchemaUndefined{}}, false)
}
