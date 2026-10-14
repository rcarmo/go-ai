package durable

import (
	"strings"
	"testing"
)

func TestCodemode120DeclarationCommentsAndFallback(t *testing.T) {
	schema := JSON{"type": "object", "properties": map[string]any{"n": JSON{"type": "integer", "minimum": 1, "maximum": 10, "description": "count\nsecond line"}, "pattern": JSON{"type": "string", "pattern": "a\nb"}}}
	got := codemodeSchemaType(schema, 0)
	for _, part := range []string{"// count\n  // second line", "// @integer @minimum 1 @maximum 10", "// @pattern a\\u000ab"} {
		if !strings.Contains(got, part) {
			t.Fatal(part, got)
		}
	}
	if strings.Contains(got, "[key: string]") {
		t.Fatal("implicit index on named object", got)
	}
	small := JSON{"type": "object", "properties": map[string]any{"n": JSON{"type": "integer", "minimum": 1}}}
	if got := codemodeSchemaTypeBudget(small, 0, 20); got != "{ n?: number; }" {
		t.Fatal("tag fallback", got)
	}
	if got := codemodeSchemaTypeBudget(small, 0, 10); got != "unknown" {
		t.Fatal(got)
	}
}
