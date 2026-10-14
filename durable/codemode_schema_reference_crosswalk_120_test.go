package durable

import "testing"

func TestCodemode120PinnedRecursiveReferenceCase(t *testing.T) {
	schema := JSON{"type": "object", "properties": map[string]any{"item": JSON{"$ref": "#/$defs/Item"}, "legacy": JSON{"$ref": "#/definitions/Legacy"}, "remote": JSON{"$ref": "https://example.com/schema.json"}}, "required": []any{"item"}, "$defs": JSON{"Item": JSON{"type": "object", "properties": map[string]any{"id": JSON{"type": "string"}, "parent": JSON{"$ref": "#/$defs/Item"}}, "required": []any{"id"}}}, "definitions": JSON{"Legacy": JSON{"enum": []any{"a", "b"}}}}
	want := `{ item: { id: string; parent?: unknown; }; legacy?: "a" | "b"; remote?: unknown; }`
	if got := codemodeSchemaType(schema, 0); got != want {
		t.Fatal(got, want)
	}
}
