package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:302–308; the supplemental pinned Coerce call also
// confirms that the unresolved value remains a string before validation.
func TestSourceV160SchemaCompiledUnresolvedParse(t *testing.T) {
	compiled, err := goai.CompileJSONSchema(json.RawMessage(v160DanglingSchema), goai.JSONSchemaCompileOptions{RejectUnresolvedRefs: true})
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"t": "1"}
	coerced, err := compiled.Coerce(input)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StandaloneJSON(t, coerced, `{"t":"1"}`)
	parsed, diagnostics, err := compiled.Parse(input)
	if err != nil || parsed != nil {
		t.Fatalf("parse = %#v, %v; want diagnostic rejection", parsed, err)
	}
	assertV160StructuredDiagnostics(t, diagnostics, `[{"path":"/t","keyword":"$ref","message":"unresolved reference #/definitions/Missing"}]`)
	assertV160StandaloneJSON(t, input, `{"t":"1"}`)
}

// validator.test.ts:310–315. This is the exact JSON emitted by the pinned
// Type.Object({a: Type.Ref("A")}); no TypeBox symbol handling is needed.
func TestSourceV160SchemaCompiledTypeBoxRef(t *testing.T) {
	const schema = `{"type":"object","required":["a"],"properties":{"a":{"$ref":"A"}}}`
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{RejectUnresolvedRefs: true})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := compiled.Explain(json.RawMessage(`{"a":{"x":"1"}}`))
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, diagnostics, `[{"path":"/a","keyword":"$ref","message":"unresolved reference A"}]`)
}

// Supplemental: use the prepared registry after its caller's bytes and map
// have changed, then alternate strict and coercing calls on the same handle.
func TestSchemaCompiledCoercionRegistryOwnership(t *testing.T) {
	const host = "https://example.com/schemas/defs/number.json"
	schema := json.RawMessage(`{"$id":"https://example.com/schemas/input.json","type":"object","properties":{"n":{"$ref":"defs/number.json"}}}`)
	resource := json.RawMessage(`{"type":"integer"}`)
	resources := map[string]json.RawMessage{host: resource}
	compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{Resources: resources}})
	if err != nil {
		t.Fatal(err)
	}
	clear(schema)
	clear(resource)
	delete(resources, host)
	input := map[string]any{"n": "42"}
	for range 2 {
		if ok, err := compiled.Check(input); ok || err != nil {
			t.Fatalf("check = %t, %v; want strict rejection", ok, err)
		}
		coerced, err := compiled.Coerce(input)
		if err != nil {
			t.Fatal(err)
		}
		assertV160StandaloneJSON(t, coerced, `{"n":42}`)
		parsed, diagnostics, err := compiled.Parse(json.RawMessage(`{"n":"42"}`))
		if err != nil {
			t.Fatal(err)
		}
		assertV160StandaloneJSON(t, parsed, `{"n":42}`)
		assertV160StructuredDiagnostics(t, diagnostics, `[]`)
		assertV160StandaloneJSON(t, input, `{"n":"42"}`)
	}
}
