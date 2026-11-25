package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:189–200. The same root reference accepts the complete
// tree and identifies the missing property in the innermost child.
func TestSourceV160SchemaRecursiveRootReference(t *testing.T) {
	const tree = `{"type":"object","properties":{"value":{"type":"number"},"children":{"type":"array","items":{"$ref":"#"}}},"required":["value"]}`
	t.Run("L196-complete-tree", func(t *testing.T) {
		assertV160SchemaCheck(t, tree, `{"value":1,"children":[{"value":2,"children":[{"value":3}]}]}`, nil, "", false, true)
	})
	t.Run("L197-deep-missing-field", func(t *testing.T) {
		assertV160MessageRecords(t, tree, `{"value":1,"children":[{"value":2,"children":[{}]}]}`,
			`[{"path":"/children/0/children/0/value","keyword":"required","message":"missing"}]`)
	})
}

// validator.test.ts:203–218. The native coercing parse assertion at L217
// lives in the standalone suite.
func TestSourceV160SchemaRelativeRootReference(t *testing.T) {
	const schema = `{
		"$id":"schemas/tool.json","type":"object",
		"properties":{"count":{"$ref":"#/$defs/count"}},"required":["count"],
		"$defs":{"count":{"type":"integer"}}
	}`
	t.Run("L211-relative-root-check", func(t *testing.T) {
		assertV160CompiledCheck(t, compileV160LeafSchema(t, schema), `{"count":2}`, true)
	})
	compiled := compileV160RejectingRefs(t, schema)
	t.Run("L213-relative-root-in-reject-mode", func(t *testing.T) {
		assertV160CompiledCheck(t, compiled, `{"count":2}`, true)
	})
	t.Run("L214-resolved-type-error", func(t *testing.T) {
		issues, err := compiled.Explain(json.RawMessage(`{"count":"a"}`))
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/count","keyword":"type","message":"expected integer, got string"}]`)
	})
}

// validator.test.ts:220–242. Diagnostics must blame the unevaluated field,
// not an unresolved reference or the already evaluated count.
func TestSourceV160SchemaRelativeRootAnnotations(t *testing.T) {
	const properties = `{
		"$id":"schemas/tool.json","type":"object",
		"properties":{"count":{"$ref":"#/$defs/count"}},
		"$defs":{"count":{"type":"integer"}},"unevaluatedProperties":false
	}`
	const items = `{
		"$id":"schemas/list.json","type":"array","prefixItems":[{"$ref":"#/$defs/count"}],
		"$defs":{"count":{"type":"integer"}},"unevaluatedItems":false
	}`
	compiled := compileV160RejectingRefs(t, properties)
	t.Run("L229-properties-have-resolved-reference", func(t *testing.T) {
		assertV160CompiledCheck(t, compiled, `{"count":2}`, true)
	})
	t.Run("L230-unevaluated-field-only", func(t *testing.T) {
		issues, err := compiled.Explain(json.RawMessage(`{"count":2,"extra":1}`))
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/extra","keyword":"unevaluatedProperties","message":"unknown field"}]`)
	})
	t.Run("L240-prefix-has-resolved-reference", func(t *testing.T) {
		assertV160CompiledCheck(t, compileV160RejectingRefs(t, items), `[2]`, true)
	})
	t.Run("L241-leftover-item-fails", func(t *testing.T) {
		compiled, err := goai.CompileJSONSchema(json.RawMessage(items), goai.JSONSchemaCompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		// Reuse the default-mode validator to distinguish a leftover item
		// from a failure to resolve the prefix item's $ref.
		assertV160CompiledCheck(t, compiled, `[2]`, true)
		assertV160CompiledCheck(t, compiled, `[2,3]`, false)
		assertV160PreparedMessageRecords(t, compiled, `[2,3]`, `[{"path":"/1","keyword":"unevaluatedItems","message":"unexpected item"}]`)
	})
}

// validator.test.ts:244–267. Each nested resource's fragment resolves from
// the resource itself, even when reached by an applicator or a pointer.
func TestSourceV160SchemaNestedRelativeDiagnostics(t *testing.T) {
	const cloned = `{
		"$id":"https://example.com/root.json","allOf":[{"$ref":"#/$defs/child"}],
		"$defs":{"child":{"$id":"schemas/tool.json","$ref":"#/$defs/props","unevaluatedProperties":false,
			"$defs":{"props":{"properties":{"count":{"type":"integer"}}}}}}
	}`
	const branch = `{
		"$id":"https://example.com/root.json",
		"anyOf":[{"$id":"a/b.json","$ref":"#/$defs/n","$defs":{"n":{"type":"integer"}}},{"type":"boolean"}]
	}`
	t.Run("L257-nested-unevaluated-resource", func(t *testing.T) {
		compiled := compileV160LeafSchema(t, cloned)
		assertV160CompiledCheck(t, compiled, `{"count":2}`, true)
		assertV160CompiledCheck(t, compiled, `{"count":2,"extra":1}`, false)
		assertV160PreparedMessageRecords(t, compiled, `{"count":2,"extra":1}`,
			`[{"path":"/extra","keyword":"unevaluatedProperties","message":"unknown field"}]`)
	})
	t.Run("L264-nested-branch-type", func(t *testing.T) {
		compiled := compileV160LeafSchema(t, branch)
		assertV160CompiledCheck(t, compiled, `1`, true)
		assertV160CompiledCheck(t, compiled, `true`, true)
		assertV160CompiledCheck(t, compiled, `{}`, false)
		assertV160PreparedMessageRecords(t, compiled, `{}`, `[{"path":"","keyword":"type","message":"expected integer, got object"}]`)
	})
}

func compileV160RejectingRefs(t *testing.T, schema string) *goai.CompiledJSONSchema {
	t.Helper()
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{RejectUnresolvedRefs: true})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func assertV160CompiledCheck(t *testing.T, compiled *goai.CompiledJSONSchema, input string, want bool) {
	t.Helper()
	if got, err := compiled.Check(json.RawMessage(input)); err != nil || got != want {
		t.Fatalf("compiled check(%s) = %t, %v; want %t", input, got, err, want)
	}
}
