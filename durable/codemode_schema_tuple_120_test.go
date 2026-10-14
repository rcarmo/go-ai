package durable

import "testing"

func TestCodemode120PinnedArrayTupleDeclarations(t *testing.T) {
	for _, tc := range []struct {
		schema JSON
		want   string
	}{{JSON{"type": "array", "items": JSON{"type": "string"}}, "Array<string>"}, {JSON{"type": "array", "prefixItems": []any{JSON{"type": "string"}, JSON{"type": "integer"}}}, "[string, number]"}, {JSON{"type": "array"}, "unknown[]"}, {JSON{"items": []any{JSON{"type": "boolean"}, false}}, "[boolean, never]"}, {JSON{"prefixItems": []any{JSON{"type": "number"}}, "items": JSON{"type": "string"}}, "Array<string>"}, {JSON{"items": true}, "Array<unknown>"}, {JSON{"properties": map[string]any{"n": JSON{"type": "integer"}}, "additionalProperties": false}, "{\n  // @integer\n  n?: number;\n}"}} {
		got := codemodeSchemaType(tc.schema, 0)
		if got != tc.want {
			t.Fatal(tc.schema, got, tc.want)
		}
	}
}
