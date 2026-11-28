package goai_test

import (
	"encoding/json"
	"math/big"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Locator: packages/schema/test/builders.test.ts. Go has no builder or static
// generic surface, so these assertions use the builder's exact public JSON
// projection through CompileJSONSchema. Type-only assertions remain partial.
func TestSchemaBuilderJSONProjection(t *testing.T) {
	t.Run("plain object", func(t *testing.T) {
		compiled := compileProjection(t, `{"type":"object","properties":{"path":{"type":"string","description":"File path","minLength":1},"offset":{"type":"integer","minimum":1},"mode":{"enum":["read","write"]}},"required":["path","mode"],"additionalProperties":false}`)
		assertProjectionCheck(t, compiled, json.RawMessage(`{"path":"x","mode":"read","offset":2}`), true)
		assertProjectionCheck(t, compiled, json.RawMessage(`{"path":"x","mode":"append"}`), false)
	})

	t.Run("leaf and container kinds", func(t *testing.T) {
		for _, tc := range []struct {
			name, schema   string
			valid, invalid any
		}{
			{"string", `{"type":"string"}`, "x", 1},
			{"number", `{"type":"number"}`, 1.5, "x"},
			{"integer", `{"type":"integer"}`, 3, 3.5},
			{"boolean", `{"type":"boolean"}`, true, 1},
			{"null", `{"type":"null"}`, nil, false},
			{"unknown", `{}`, map[string]any{"x": 1}, nil},
			{"literal", `{"const":3}`, 3, 2},
			{"bytes", `{"type":"bytes","maxLength":8}`, []byte{1, 2}, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}},
			{"array", `{"type":"array","items":{"type":"string"},"maxItems":2}`, []any{"a"}, []any{"a", "b", "c"}},
			{"map", `{"type":"object","additionalProperties":{"type":"number"}}`, map[string]any{"x": 1}, map[string]any{"x": "1"}},
			{"tuple", `{"type":"array","prefixItems":[{"type":"string"},{"type":"number"}],"items":false,"minItems":2}`, []any{"x", 1}, []any{"x"}},
			{"nullable", `{"anyOf":[{"type":"string"},{"type":"null"}]}`, nil, 2},
			{"open object", `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`, map[string]any{"a": "x", "extra": true}, map[string]any{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				compiled := compileProjection(t, tc.schema)
				assertProjectionCheck(t, compiled, tc.valid, true)
				if tc.name != "unknown" {
					assertProjectionCheck(t, compiled, tc.invalid, false)
				}
			})
		}
	})

	t.Run("bigint and unsafe", func(t *testing.T) {
		metric := compileProjection(t, `{"anyOf":[{"type":"bigint","minimum":0},{"type":"null"}]}`)
		assertProjectionCheck(t, metric, big.NewInt(3), true)
		assertProjectionCheck(t, metric, big.NewInt(-1), false)
		assertProjectionCheck(t, metric, 3, false)
		unconstrained := compileProjection(t, `{}`)
		assertProjectionCheck(t, unconstrained, "not usage", true)
		level := compileProjection(t, `{"type":"string","enum":["low","high"]}`)
		assertProjectionCheck(t, level, "medium", false)
	})

	t.Run("recursive", func(t *testing.T) {
		compiled := compileProjection(t, `{"type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"Tree"}}},"required":["name","children"],"additionalProperties":false,"$id":"Tree"}`)
		assertProjectionCheck(t, compiled, json.RawMessage(`{"name":"a","children":[{"name":"b","children":[]}]}`), true)
		issues, err := compiled.Explain(json.RawMessage(`{"name":"a","children":[{"name":"b","children":[{"name":1,"children":[]}]}]}`))
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/children/0/children/0/name","keyword":"type","message":"expected string, got integer"}]`)
	})

	t.Run("nested recursive ids", func(t *testing.T) {
		compiled := compileProjection(t, `{"type":"object","properties":{"tree":{"type":"object","properties":{"name":{"type":"string"},"children":{"type":"array","items":{"$ref":"Tree"}}},"required":["name","children"],"additionalProperties":false,"$id":"Tree"},"list":{"type":"object","properties":{"value":{"type":"number"},"next":{"anyOf":[{"$ref":"List"},{"type":"null"}]}},"required":["value","next"],"additionalProperties":false,"$id":"List"}},"required":["tree"],"additionalProperties":false}`)
		root := json.RawMessage(`{"tree":{"name":"a","children":[{"name":"b","children":[]}]},"list":{"value":1,"next":{"value":2,"next":null}}}`)
		assertProjectionCheck(t, compiled, root, true)
		assertProjectionCheck(t, compiled, json.RawMessage(`{"tree":{"name":"a","children":[]},"list":{"value":1,"next":{"value":"2","next":null}}}`), false)
	})

	t.Run("ordered diagnostics", func(t *testing.T) {
		compiled := compileProjection(t, `{"type":"object","properties":{"name":{"type":"string","minLength":1},"tags":{"type":"array","items":{"type":"string"},"uniqueItems":true}},"required":["name"],"additionalProperties":false}`)
		issues, err := compiled.Explain(json.RawMessage(`{"name":"","tags":["x","x"],"other":1}`))
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/name","keyword":"minLength","message":"expected at least 1 characters"},{"path":"/tags","keyword":"uniqueItems","message":"expected unique items"},{"path":"/other","keyword":"additionalProperties","message":"unknown field"}]`)
	})
}

// Locator: packages/schema/test/typebox-static.test.ts. Runtime assertions use
// TypeBox's emitted string-key JSON projection. TypeScript optional/readonly and
// static inference assertions are intentionally partial: Go has no TypeBox
// symbol keys or compile-time generic schema types to inspect.
func TestSchemaTypeBoxStaticJSONProjection(t *testing.T) {
	schema := compileProjection(t, `{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"id":{"type":"string"},"tag":{"const":"x"}},"required":["path","id"]}`)
	assertProjectionCheck(t, schema, json.RawMessage(`{"path":"p","id":"i"}`), true)
	assertProjectionCheck(t, schema, json.RawMessage(`{"path":"p","offset":"1","id":"i"}`), false)

	nested := compileProjection(t, `{"type":"object","properties":{"edits":{"type":"array","items":{"type":"object","properties":{"oldText":{"type":"string"},"newText":{"type":"string"}},"required":["oldText","newText"]}},"mode":{"anyOf":[{"const":"text"},{"const":"binary"},{"type":"null"}]},"env":{"type":"object","patternProperties":{"^(.*)$":{"anyOf":[{"type":"string"},{"type":"number"}]}}},"counts":{"type":"object","patternProperties":{"^-?(?:0|[1-9][0-9]*)$":{"type":"boolean"}}}},"required":["edits","mode","env","counts"]}`)
	assertProjectionCheck(t, nested, json.RawMessage(`{"edits":[{"oldText":"a","newText":"b"}],"mode":null,"env":{"x":1},"counts":{"3":true}}`), true)

	tuple := compileProjection(t, `{"type":"array","prefixItems":[{"type":"number"},{"anyOf":[{"type":"number"},{"type":"null"}]}],"items":false,"minItems":2,"maxItems":2}`)
	assertProjectionCheck(t, tuple, json.RawMessage(`[1,null]`), true)
	assertProjectionCheck(t, tuple, json.RawMessage(`[1]`), false)

	mappedKinds := compileProjection(t, `{"type":"object","properties":{"kind":{"enum":["a","b",3]},"tuple":{"type":"array","prefixItems":[{"type":"number"},{"type":"null"}],"items":false,"minItems":2},"big":{"type":"bigint","minimum":0},"none":{"type":"undefined"}},"required":["kind","tuple","big","none"]}`)
	value := map[string]any{"kind": "b", "tuple": []any{1, nil}, "big": big.NewInt(1), "none": goai.SchemaUndefined{}}
	assertProjectionCheck(t, mappedKinds, value, true)

	parsed, diagnostics, err := compileProjection(t, `{"type":"object","properties":{"n":{"type":"number"},"s":{"type":"string"}},"required":["n"]}`).Parse(json.RawMessage(`{"n":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 0 {
		t.Fatalf("parse diagnostics = %#v", diagnostics)
	}
	if encoded, err := json.Marshal(parsed); err != nil || string(encoded) != `{"n":1}` {
		t.Fatalf("parsed = %s, %v", encoded, err)
	}
}

func compileProjection(t *testing.T, schema string) *goai.CompiledJSONSchema {
	t.Helper()
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func assertProjectionCheck(t *testing.T, compiled *goai.CompiledJSONSchema, value any, want bool) {
	t.Helper()
	got, err := compiled.Check(value)
	if err != nil || got != want {
		t.Fatalf("Check(%#v) = %t, %v; want %t", value, got, err, want)
	}
}
