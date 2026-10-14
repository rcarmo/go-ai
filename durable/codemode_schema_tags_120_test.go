package durable

import (
	"strings"
	"testing"
)

func TestCodemode120PinnedConstraintTagGroupingAndEscapes(t *testing.T) {
	schema := JSON{"type": "object", "properties": map[string]any{"count": JSON{"type": "integer", "minimum": 0, "maximum": 30000, "default": 0}, "pattern": JSON{"type": "string", "pattern": "one\rtwo\nthree\u2028four\u2029five"}, "long": JSON{"type": "string", "pattern": strings.Repeat("x", 120), "description": "still described"}}}
	text := codemodeSchemaType(schema, 0)
	for _, want := range []string{"// @integer @minimum 0 @maximum 30000 @default 0", "// @pattern one\\u000dtwo\\u000athree\\u2028four\\u2029five", "// still described"} {
		if !strings.Contains(text, want) {
			t.Fatal(text, want)
		}
	}
	if strings.Contains(text, strings.Repeat("x", 120)) {
		t.Fatal("long tag not dropped")
	}
}
