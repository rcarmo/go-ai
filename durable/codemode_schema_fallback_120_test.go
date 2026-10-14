package durable

import (
	"strings"
	"testing"
)

func TestCodemode120DeclarationFallbackPreservesDescriptionAndLiterals(t *testing.T) {
	schema := JSON{"type": "object", "properties": map[string]any{"s": JSON{"const": "two  spaces", "description": "@minimum is prose", "minLength": 1}}}
	untagged := codemodeSchemaValueMode(schema, 0, false)
	if !strings.Contains(untagged, "// @minimum is prose") || !strings.Contains(untagged, `"two  spaces"`) {
		t.Fatal(untagged)
	}
	got := codemodeSchemaTypeBudget(schema, 0, codemodeTextUnits(untagged))
	if got != untagged {
		t.Fatal("fallback altered prose/literal", got, untagged)
	}
	if got := codemodeSchemaTypeBudget(schema, 0, codemodeTextUnits(untagged)-1); got != "unknown" {
		t.Fatal("description silently removed", got)
	}
}
