package goai

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// Exact conformance assertions use the production non-coercing membership
// checker: Tool.parse intentionally converts inputs that check() rejects.
func TestValidationResourceDependency(t *testing.T) {
	var f struct {
		Upstream   string
		Dependency []struct {
			Name          string
			Schema, Input json.RawMessage
			Valid         bool
		}
	}
	data, err := os.ReadFile("tests/testdata/upstream-main/validation-resources.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.Dependency) != 54 {
		t.Fatal("unexpected pinned resource fixture")
	}
	for _, tc := range f.Dependency {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			var schema map[string]interface{}
			var input interface{}
			if err := json.Unmarshal(tc.Schema, &schema); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(tc.Input, &input); err != nil {
				t.Fatal(err)
			}
			v := newToolSchemaValidator(schema)
			if got := v.schemaAcceptsValue(input, schema); got != tc.Valid {
				t.Fatalf("membership=%v want %v", got, tc.Valid)
			}
		})
	}
}

func TestValidationResourceIndexWork(t *testing.T) {
	defs := make(map[string]interface{})
	for i := 0; i < 128; i++ {
		defs[fmt.Sprint(i)] = map[string]interface{}{"$id": fmt.Sprintf("id%d", i), "type": "integer"}
	}
	root := map[string]interface{}{"$defs": defs}
	v := newToolSchemaValidator(root)
	v.work = &toolSchemaWork{}
	if !v.schemaAcceptsValue(1, root) || v.resources != nil {
		t.Fatal("reference-free check indexed identifiers")
	}
	for i := 0; i < 512; i++ {
		if v.resolve(fmt.Sprintf("id%d", i%128)) == nil {
			t.Fatal("missing known resource")
		}
	}
	if v.work.indexNodes != 129 {
		t.Fatalf("repeated/missing indexing: %d nodes", v.work.indexNodes)
	}
	if len(v.refs) != 128 || len(v.resources.bases) != 129 || len(v.resources.orders) != 0 {
		t.Fatal("unexpected resource cache or retained ordering metadata")
	}
}

// The compiler visits schema map entries in JavaScript property order. Indexing
// must retain that order for reachable property maps, not annotation data.
func TestValidationResourceCompilerOrderRetention(t *testing.T) {
	raw := []byte(`{"properties":{"z":{"type":"string"},"10":true,"2":false,"a":{"properties":{"y":true,"x":false}}},"patternProperties":{"z":true,"a":true},"$defs":{"unused":{"properties":{"q":true,"b":false}}},"default":{"z":1,"a":2}}`)
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	v := newToolSchemaValidator(root, raw)
	r := v.resourceIndex()
	props := root["properties"].(map[string]interface{})
	if got := r.keys(props); !reflect.DeepEqual(got, []string{"2", "10", "z", "a"}) {
		t.Fatalf("property order=%v", got)
	}
	inner := props["a"].(map[string]interface{})["properties"].(map[string]interface{})
	if got := r.keys(inner); !reflect.DeepEqual(got, []string{"y", "x"}) {
		t.Fatalf("nested property order=%v", got)
	}
	patterns := root["patternProperties"].(map[string]interface{})
	if got := r.keys(patterns); !reflect.DeepEqual(got, []string{"z", "a"}) {
		t.Fatalf("pattern order=%v", got)
	}
	unused := root["$defs"].(map[string]interface{})["unused"].(map[string]interface{})["properties"].(map[string]interface{})
	if got := r.keys(unused); !reflect.DeepEqual(got, []string{"q", "b"}) {
		t.Fatalf("referencable definition order=%v", got)
	}
	annotation := root["default"].(map[string]interface{})
	if got := r.keys(annotation); !reflect.DeepEqual(got, []string{"a", "z"}) {
		t.Fatalf("annotation data order was retained: %v", got)
	}
	if len(r.orders) != 0 || v.schemaRaw != nil {
		t.Fatal("retained full ordering table or raw schema")
	}
}

// A JSON Pointer can select non-indexed data, while inheriting the last
// indexed resource's base. Resolve must retain that base on cache hits.
func TestValidationResolvedReferenceCarriesPointerBase(t *testing.T) {
	raw := []byte(`{"$id":"https://example.test/outer","$defs":{"inner":{"$id":"https://example.test/inner","default":{"$ref":"#/$defs/number"},"$defs":{"number":{"type":"number"}},"properties":{"x":{"$ref":"#/default"}}}},"properties":{"x":{"$ref":"https://example.test/inner"}}}`)
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	v := newToolSchemaValidator(root, raw)
	inner := root["$defs"].(map[string]interface{})["inner"].(map[string]interface{})
	from := inner["properties"].(map[string]interface{})["x"].(map[string]interface{})
	want := inner["default"].(map[string]interface{})
	for i := 0; i < 2; i++ {
		target, base := v.resolveWithBase("#/default", from)
		m, ok := target.(map[string]interface{})
		if !ok || reflect.ValueOf(m).Pointer() != reflect.ValueOf(want).Pointer() || base != "https://example.test/inner" {
			t.Fatalf("target=%v base=%q", target, base)
		}
	}
	_, base := v.resolveWithBase("#/$defs/inner/default", root)
	if base != "https://example.test/inner" {
		t.Fatalf("nested pointer base=%q", base)
	}
	if target, _ := v.resolveWithBase("#/missing", from); target != nil {
		t.Fatalf("unresolved=%v", target)
	}
}

// Equal relative references in different resources must not share a cache
// entry: each base names its own schema, even after the cache is warm.
func TestValidationResolvedReferenceCacheSeparatesBases(t *testing.T) {
	raw := []byte(`{"$id":"https://example.test/root","$defs":{"a":{"$id":"https://example.test/a","$defs":{"value":{"type":"number"}}},"b":{"$id":"https://example.test/b","$defs":{"value":{"type":"string"}}}}}`)
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	v := newToolSchemaValidator(root, raw)
	for _, tc := range []struct{ base, want string }{
		{"https://example.test/a", "number"},
		{"https://example.test/b", "string"},
		{"https://example.test/a", "number"},
		{"https://example.test/b", "string"},
	} {
		target, base := v.resolveFromBase("#/$defs/value", tc.base)
		m, ok := target.(map[string]interface{})
		if !ok || m["type"] != tc.want || base != tc.base {
			t.Fatalf("base=%q target=%v targetBase=%q", tc.base, target, base)
		}
	}
}
