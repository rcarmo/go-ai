package durable

import (
	"fmt"
	"strings"
	"testing"
)

func TestCodemode120DeclarationCharacterBudget(t *testing.T) {
	properties := map[string]any{}
	for i := 0; i < 50; i++ {
		properties[fmt.Sprintf("field%d", i)] = JSON{"type": "string"}
	}
	schema := JSON{"type": "object", "properties": properties}
	if got := codemodeSchemaTypeBudget(schema, 0, 100); got != "unknown" {
		t.Fatal("char budget exceeded", got)
	}
	if got := codemodeSchemaType(schema, 0); !strings.Contains(got, "field49?: string;") {
		t.Fatal(got)
	}
	if got := codemodeSchemaType(JSON{"const": strings.Repeat("x", 70000)}, 0); got != "unknown" {
		t.Fatal("oversized literal")
	}
	cycle := JSON{}
	cycle["properties"] = map[string]any{"self": cycle}
	if got := codemodeSchemaType(cycle, 0); got != "unknown" {
		t.Fatal("cyclic host schema")
	}
}
