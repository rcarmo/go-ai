package durable

import (
	"reflect"
	"testing"
)

// The original v1.5.2 tests require identity, not merely equal contents.
func TestSourceV152ResolveCodemodeSchemaRef(t *testing.T) {
	item := JSON{"type": "string"}
	root := JSON{"$defs": JSON{"item": item, "a/b": item, "c~d": item, "e f": item}}
	for _, ref := range []string{"#", "#/$defs/item", "#/$defs/a~1b", "#/$defs/c~0d", "#/$defs/e%20f"} {
		want := item
		if ref == "#" {
			want = root
		}
		got, found := ResolveCodemodeSchemaRef(ref, root)
		if !found || reflect.ValueOf(got).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Fatalf("%s did not resolve to the original map: %v, %v", ref, got, found)
		}
	}
	for _, ref := range []string{"other.json#/$defs/item", "#/$defs/missing", "#/$defs/%E0%A4%A"} {
		if got, found := ResolveCodemodeSchemaRef(ref, root); found || got != nil {
			t.Fatalf("%s resolved: %v", ref, got)
		}
	}
	root["$ref"] = "#/$defs/e%20f"
	if rendered := codemodeSchemaType(root, 0); rendered != "string" {
		t.Fatal(rendered)
	}
}
