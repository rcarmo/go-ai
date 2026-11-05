package goai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// A private, deliberately incomplete compiler traversal: unsupported paths
// must be explicit, not treated as a successful public preflight.
func TestValidationPlainPatternTraversal(t *testing.T) {
	for _, tc := range []struct{ name, raw, pattern string }{
		{"root-first", `{"pattern":"[","properties":{"p":{"pattern":"*"}}}`, `[`},
		{"property-first", `{"properties":{"p":{"pattern":"*"}},"allOf":[{"pattern":"["}]}`, `*`},
		{"insertion", `{"properties":{"z":{"pattern":"*"},"a":{"pattern":"["}}}`, `*`},
		{"integer-order", `{"properties":{"z":{"pattern":"*"},"10":{"pattern":"["},"2":{"pattern":"("}}}`, `(`},
		{"pattern-key", `{"patternProperties":{"[":{"pattern":"*"}}}`, `[`},
		{"property-names", `{"propertyNames":{"pattern":"["}}`, `[`},
		{"dependent", `{"dependentSchemas":{"x":{"pattern":"["}}}`, `[`},
		{"dependency", `{"dependencies":{"x":{"pattern":"["}}}`, `[`},
		{"tuple-rest", `{"items":[true],"additionalItems":{"pattern":"["}}`, `[`},
		{"prefix", `{"prefixItems":[{"pattern":"["}]}`, `[`},
		{"contains", `{"contains":{"pattern":"["}}`, `[`},
		{"reference", `{"$defs":{"x":{"pattern":"["}},"properties":{"x":{"$ref":"#/$defs/x"}}}`, `[`},
		{"recursive-reference", `{"$defs":{"x":{"properties":{"next":{"$ref":"#/$defs/x"},"value":{"pattern":"["}}}},"$ref":"#/$defs/x"}`, `[`},
		{"union", `{"anyOf":[{"type":"number"},{"pattern":"["}]}`, `[`},
		{"then-before-if", `{"if":{"pattern":"["},"then":{"pattern":"*"}}`, `*`},
		{"unused-definition", `{"$defs":{"x":{"pattern":"["}}}`, ``},
		{"annotation", `{"default":{"pattern":"["}}`, ``},
		{"if-alone", `{"if":{"pattern":"["}}`, ``},
		{"then-alone", `{"then":{"pattern":"["}}`, ``},
		{"additional-alone", `{"additionalItems":{"pattern":"["}}`, ``},
		{"bytes-single", `{"type":"bytes","properties":{"x":{"pattern":"["}}}`, ``},
		{"object-single-type-array", `{"type":["object"],"properties":{"x":{"pattern":"["}}}`, `[`},
		{"additional-schema-object", `{"additionalProperties":{"pattern":"["}}`, `[`},
		{"items-schema-object", `{"items":{"pattern":"["}}`, `[`},
		{"const-union-annotation", `{"anyOf":[{"const":1,"default":{"pattern":"["}},{"const":2}]}`, ``},
		{"tagged-ref", `{"$defs":{"a":{"type":"object","properties":{"kind":{"const":"a"},"x":{"pattern":"["}},"required":["kind"]},"b":{"type":"object","properties":{"kind":{"const":"b"}},"required":["kind"]}},"anyOf":[{"$ref":"#/$defs/a"},{"$ref":"#/$defs/b"}]}`, `[`},
		{"dependency-array", `{"dependencies":{"x":[{"pattern":"["}]}}`, ``},
		{"unresolved-reject", `{"$ref":"#/$defs/missing"}`, ``},
		{"union-bad-before-unresolved", `{"anyOf":[{"pattern":"["},{"$ref":"#/$defs/missing"}]}`, `[`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var root map[string]interface{}
			if err := json.Unmarshal([]byte(tc.raw), &root); err != nil {
				t.Fatal(err)
			}
			v := newToolSchemaValidator(root, []byte(tc.raw))
			err := v.checkPlainSchemaPatterns()
			if tc.pattern == "" {
				if err != nil {
					t.Fatalf("unexpected %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "invalid regular expression: "+tc.pattern) {
				t.Fatalf("got %v want invalid %q", err, tc.pattern)
			}
		})
	}
}

func TestValidationPlainPatternTraversalBounds(t *testing.T) {
	// Indexing sees ignored literal data too: reject cycles before it recurses.
	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic
	shared := map[string]interface{}{"x": true}
	wide := make([]interface{}, maxSchemaPatternValues)
	for _, root := range []map[string]interface{}{
		{"const": cyclic},
		{"default": []interface{}{shared, shared}},
		{"default": wide},
		{"default": strings.Repeat("x", maxSchemaPatternRawBytes+1)},
		{"default": json.Number(strings.Repeat("9", maxSchemaPatternRawBytes+1))},
	} {
		v := newToolSchemaValidator(root)
		if err := v.checkPlainSchemaPatterns(); !errors.Is(err, errSchemaPatternPreflightUnsupported) || v.resources != nil {
			t.Fatalf("got %v resources=%v; want bounded stop before indexing", err, v.resources != nil)
		}
	}
	root := map[string]interface{}{}
	current := root
	for i := 0; i <= maxSchemaPatternDepth; i++ {
		next := map[string]interface{}{}
		current["default"] = next
		current = next
	}
	if err := newToolSchemaValidator(root).checkPlainSchemaPatterns(); !errors.Is(err, errSchemaPatternPreflightUnsupported) {
		t.Fatalf("deep input got %v", err)
	}
	many := make([]interface{}, maxSchemaPatternNodes+1)
	for i := range many {
		many[i] = map[string]interface{}{}
	}
	if err := newToolSchemaValidator(map[string]interface{}{"allOf": many}).checkPlainSchemaPatterns(); !errors.Is(err, errSchemaPatternPreflightUnsupported) {
		t.Fatalf("too many reachable nodes got %v", err)
	}
	v := newToolSchemaValidator(map[string]interface{}{"pattern": "a"})
	v.patternDeadline = time.Now().Add(-time.Second)
	if err := v.checkPlainSchemaPatterns(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired syntax budget got %v", err)
	}
}

func TestValidationPlainPatternTraversalStops(t *testing.T) {
	for _, raw := range []string{
		`{"$dynamicRef":"#x"}`,
		`{"unevaluatedProperties":true}`,
		`{"properties":{"x":{"unevaluatedItems":false}}}`,
		`{"items":null}`,
		// Pinned compiler follows this pointer with the inner resource base;
		// the native resolver does not retain a base for data-only targets.
		`{"$id":"https://example.test/outer","$defs":{"inner":{"$id":"https://example.test/inner","default":{"$ref":"#/$defs/bad"},"$defs":{"bad":{"pattern":"["}},"properties":{"x":{"$ref":"#/default"}}}},"properties":{"x":{"$ref":"https://example.test/inner"}}}`,
	} {
		var root map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &root); err != nil {
			t.Fatal(err)
		}
		v := newToolSchemaValidator(root, []byte(raw))
		if err := v.checkPlainSchemaPatterns(); !errors.Is(err, errSchemaPatternPreflightUnsupported) {
			t.Fatalf("schema %s got %v; want unsupported", raw, err)
		}
	}
}
