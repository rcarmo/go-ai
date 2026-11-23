package goai

import (
	"encoding/json"
	"fmt"
	"strings"
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
			// coerce.test.ts:414–437 expects every nested required failure
			// followed by the leaf type error. Go formats the paths for tools.
			lines := make([]string, 0, depth+2)
			for i := 0; i <= depth; i++ {
				lines = append(lines, "  - s"+strings.Repeat(".c", i)+".a: missing")
			}
			lines = append(lines, "  - s"+strings.Repeat(".c", depth)+".x: expected object, got integer")
			if err == nil || err.Error() != strings.Join(lines, "\n") {
				t.Fatalf("unexpected validation: %v; want\n%s", err, strings.Join(lines, "\n"))
			}
			if got := v.work.diagnosticRefs; got != depth+2 {
				t.Fatalf("reference explanation: %d visits, want %d", got, depth+2)
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

// Supplemental identity control for validator.test.ts:443–453. The public tool
// API's normalization may copy each branch, so drive the diagnostic walk
// directly here with the exact same failing object at both reference paths.
func TestValidationDiagnosticRefMemoKeepsPaths(t *testing.T) {
	var schema map[string]any
	const raw = `{"type":"object","properties":{"a":{"$ref":"#/$defs/P"},"b":{"$ref":"#/$defs/P"}},"$defs":{"P":{"type":"object","properties":{"n":{"type":"integer"}}}}}`
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	shared := map[string]any{"n": "x"}
	v := newToolSchemaValidator(schema, []byte(raw))
	issues := v.appendIssues(nil, map[string]any{"a": shared, "b": shared}, schema, "", make(map[toolSchemaMemoKey]bool), false)
	const want = "  - a.n: expected integer, got string\n  - b.n: expected integer, got string"
	if got := issues.Error(); got != want {
		t.Fatalf("diagnostics = %q; want %q", got, want)
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
