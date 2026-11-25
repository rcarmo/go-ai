package goai_test

import "testing"

// coerce.test.ts:119–138. The prepared methods explicitly select strict
// validation or converting parse. Static types and JS identity are separate.
func TestSourceV160SchemaPreparedValidateWithoutCoercion(t *testing.T) {
	const schema = `{"type":"object","properties":{"count":{"type":"number"},"tags":{"type":"array","items":{"type":"string"}}},"required":["count","tags"],"additionalProperties":false}`
	const accepted = `{"count":42,"tags":["a"]}`
	const rejected = `[{"path":"/count","keyword":"type","message":"expected number, got string"},{"path":"/tags","keyword":"type","message":"expected array, got string"}]`
	compiled := compileV160LeafSchema(t, schema)
	input := map[string]any{"count": "42", "tags": "a"}
	t.Run("supplemental-parse-converts-the-input", func(t *testing.T) {
		value, issues, err := compiled.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		assertV160StandaloneJSON(t, value, accepted)
		assertV160StructuredDiagnostics(t, issues, `[]`)
		assertV160StandaloneJSON(t, input, `{"count":"42","tags":"a"}`)
	})
	t.Run("L124-valid-value-and-no-diagnostics", func(t *testing.T) {
		valid := map[string]any{"count": 42, "tags": []any{"a"}}
		value, issues, err := compiled.Validate(valid)
		if err != nil {
			t.Fatal(err)
		}
		assertV160StandaloneJSON(t, value, accepted)
		assertV160StructuredDiagnostics(t, issues, `[]`)
	})
	t.Run("L130-two-ordered-diagnostics-without-conversion", func(t *testing.T) {
		value, issues, err := compiled.Validate(input)
		if value != nil || err != nil {
			t.Fatalf("validate = %#v, %v; want diagnostic rejection", value, err)
		}
		assertV160StructuredDiagnostics(t, issues, rejected)
		assertV160StandaloneJSON(t, input, `{"count":"42","tags":"a"}`)
	})
}
