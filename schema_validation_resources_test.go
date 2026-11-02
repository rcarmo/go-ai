package goai

import (
	"encoding/json"
	"fmt"
	"os"
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
