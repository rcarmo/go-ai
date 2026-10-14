package durable

import "testing"

// Exact expectations from pinned declarations.test.ts first three declarations.
func TestCodemode120PinnedTypeObjectAndDescriptionCases(t *testing.T) {
	for _, tc := range []struct {
		schema JSON
		want   string
	}{{JSON{"type": "string"}, "string"}, {JSON{"type": "integer"}, "number"}, {JSON{"type": []any{"string", "null"}}, "string | null"}, {JSON{"const": "a"}, `"a"`}, {JSON{"enum": []any{"a", 1, nil}}, `"a" | 1 | null`}, {JSON{"anyOf": []any{JSON{"type": "string"}, JSON{"type": "number"}}}, "string | number"}, {JSON{"anyOf": []any{JSON{"type": "string"}, JSON{}}}, "unknown"}, {JSON{"allOf": []any{JSON{"anyOf": []any{JSON{"type": "string"}, JSON{"type": "number"}}}, JSON{"const": 1}}}, "(string | number) & 1"}, {JSON{"$ref": "#/defs/x"}, "unknown"}, {JSON{"type": "object", "properties": map[string]any{"city": JSON{"type": "string"}, "max-lines": JSON{"type": "number"}}, "required": []any{"city"}, "additionalProperties": false}, `{ city: string; "max-lines"?: number; }`}, {JSON{"type": "object", "additionalProperties": JSON{"type": "number"}}, "{ [key: string]: number; }"}, {JSON{"type": "object"}, "{ [key: string]: unknown; }"}, {JSON{"type": "object", "properties": map[string]any{}, "additionalProperties": false}, "{}"}, {JSON{"type": "object", "properties": map[string]any{"weather": JSON{"type": "array", "description": "look up weather for a given list of locations", "items": JSON{"type": "object", "properties": map[string]any{"location": JSON{"type": "string"}}, "required": []any{"location"}}}}, "required": []any{"weather"}}, "{\n  // look up weather for a given list of locations\n  weather: Array<{ location: string; }>;\n}"}, {JSON{"type": "object", "properties": map[string]any{"outer": JSON{"type": "object", "description": "Outer", "properties": map[string]any{"inner": JSON{"type": "string", "description": "Inner"}}}}}, "{\n  // Outer\n  outer?: {\n    // Inner\n    inner?: string;\n  };\n}"}} {
		got := codemodeSchemaType(tc.schema, 0)
		if got != tc.want {
			t.Fatal(tc.schema, got, tc.want)
		}
	}
	if codemodeSchemaValue(true, 0) != "unknown" || codemodeSchemaValue(false, 0) != "never" {
		t.Fatal("boolean root schemas")
	}
}
