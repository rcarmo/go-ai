package goai

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Structural visit counts are a bounded-work witness, not CPU profiling.
// The public black-box oracle separately preserves the upstream assertion.
func TestValidationReferenceWorkBound(t *testing.T) {
	for _, depth := range []int{8, 40} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			var schema map[string]interface{}
			if err := json.Unmarshal([]byte(`{"type":"object","properties":{"s":{"$ref":"#/$defs/S"}},"$defs":{"S":{"anyOf":[{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["a"]},{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["b"]}]}}}`), &schema); err != nil {
				t.Fatal(err)
			}
			var value interface{} = map[string]interface{}{"x": 1}
			for i := 0; i < depth; i++ {
				value = map[string]interface{}{"c": value}
			}
			v := newToolSchemaValidator(schema)
			v.work = &toolSchemaWork{}
			_, err := v.coerceWithJSONSchema(map[string]interface{}{"s": value}, schema)
			if err == nil || err.Error() != "s.a: missing" {
				t.Fatalf("unexpected validation: %v", err)
			}
			if v.work.convertCalls > 32*(depth+2) {
				t.Fatalf("repeated conversion: %d calls at depth %d", v.work.convertCalls, depth)
			}
			// Ensure this converter really follows the additional-property refs,
			// rather than passing the work bound by ignoring their schemas.
			if len(v.converted) != depth+2 {
				t.Fatalf("reference traversal: %d memo entries, want %d", len(v.converted), depth+2)
			}
			if len(v.inputs) < len(v.converted) {
				t.Fatal("memoized input identities are not retained")
			}
			t.Logf("depth=%d converter visits=%d cached reference pairs=%d", depth, v.work.convertCalls, len(v.converted))
		})
	}
}

func TestValidationReferenceReentryNotMemoized(t *testing.T) {
	target := map[string]interface{}{"$ref": "#/$defs/S"}
	v := newToolSchemaValidator(map[string]interface{}{"$defs": map[string]interface{}{"S": target}})
	for i := 0; i < 2; i++ {
		if got := v.convertReference(1, "#/$defs/S"); got != 1 {
			t.Fatalf("cutoff changed value: %#v", got)
		}
		if len(v.converted) != 0 || len(v.converting) != 0 {
			t.Fatal("ancestor-dependent result cached or active state leaked")
		}
	}
	if v.cuts != 2 {
		t.Fatalf("expected two independent re-entry cutoffs, got %d", v.cuts)
	}
}
