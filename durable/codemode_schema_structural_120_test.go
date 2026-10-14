package durable

import "testing"

func TestCodemode120StructuralDeclarationTypes(t *testing.T) {
	for _, tc := range []struct {
		schema JSON
		want   string
	}{{JSON{"const": "fixed"}, `"fixed"`}, {JSON{"const": nil}, "null"}, {JSON{"type": []any{"string", "null"}}, "string | null"}, {JSON{"anyOf": []any{JSON{"type": "number"}, JSON{"type": "boolean"}}}, "number | boolean"}, {JSON{"allOf": []any{JSON{"anyOf": []any{JSON{"type": "string"}, JSON{"type": "null"}}}, JSON{"type": "number"}}}, "(string | null) & number"}, {JSON{"type": "object", "additionalProperties": false}, "{}"}, {JSON{"type": "object", "additionalProperties": JSON{"type": "number"}}, "{ [key: string]: number; }"}, {JSON{"type": "object", "properties": map[string]any{"yes": true, "no": false}, "additionalProperties": false}, "{ no?: never; yes?: unknown; }"}} {
		got := codemodeSchemaType(tc.schema, 0)
		if got != tc.want {
			t.Fatal(tc.schema, got, tc.want)
		}
	}
	if codemodeSchemaValue(false, 0) != "never" || codemodeSchemaValue(true, 0) != "unknown" {
		t.Fatal("boolean schema")
	}
}
