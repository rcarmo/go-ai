package durable

import (
	"strings"
	"testing"
)

func TestCodemode120PinnedAnnotationCases(t *testing.T) {
	cases := []struct {
		schema JSON
		want   string
	}{
		{JSON{"type": "object", "properties": map[string]any{"timeoutMs": JSON{"type": "integer", "minimum": 0, "maximum": 30000, "default": 0, "description": "Wait in milliseconds."}, "id": JSON{"type": "string", "pattern": "^[a-z]+$", "maxLength": 8}, "tags": JSON{"type": "array", "items": JSON{"type": "string"}, "maxItems": 5, "uniqueItems": true, "deprecated": true}, "name": JSON{"type": "string"}}, "required": []any{"id"}}, "{\n  // @maxLength 8 @pattern ^[a-z]+$\n  id: string;\n  name?: string;\n  // @maxItems 5 @uniqueItems @deprecated\n  tags?: Array<string>;\n  // Wait in milliseconds.\n  // @integer @minimum 0 @maximum 30000 @default 0\n  timeoutMs?: number;\n}"},
		{JSON{"type": "object", "properties": map[string]any{"limit": JSON{"anyOf": []any{JSON{"type": "integer", "minimum": 1, "maximum": 100}, JSON{"type": "null"}}, "default": nil, "title": "Limit"}}}, "{\n  // @integer @minimum 1 @maximum 100\n  limit?: number | null;\n}"},
		{JSON{"type": "object", "properties": map[string]any{"n": JSON{"anyOf": []any{JSON{"type": "integer", "minimum": 10, "maximum": 50, "format": "int32"}, JSON{"type": "null"}}, "minimum": 0, "maximum": 20, "format": "port"}}}, "{\n  // @integer @minimum 10 @maximum 20 @format port @format int32\n  n?: number | null;\n}"},
		{JSON{"type": "object", "properties": map[string]any{"n": JSON{"type": "integer", "minimum": 0, "exclusiveMinimum": true, "maximum": 10, "exclusiveMaximum": false}, "m": JSON{"anyOf": []any{JSON{"type": "number", "maximum": 5, "exclusiveMaximum": true}, JSON{"type": "null"}}, "maximum": 8}}}, "{\n  // @maximum 8 @exclusiveMaximum 5\n  m?: number | null;\n  // @integer @exclusiveMinimum 0 @maximum 10\n  n?: number;\n}"},
		{JSON{"type": "object", "properties": map[string]any{"config": JSON{"type": "object", "default": map[string]any{"text": strings.Repeat("x", 100)}}}}, "{ config?: { [key: string]: unknown; }; }"},
		{JSON{"type": "object", "properties": map[string]any{"sep": JSON{"type": "string", "pattern": "^[\r\n\u2028\u2029]$", "default": "\u2028", "description": "a\u2028b"}}}, "{\n  // a\n  // b\n  // @pattern ^[\\u000d\\u000a\\u2028\\u2029]$ @default \"\\u2028\"\n  sep?: string;\n}"},
	}
	for _, tc := range cases {
		if got := codemodeSchemaType(tc.schema, 0); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
