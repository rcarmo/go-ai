package goai_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Translate every group of the byte-identical pinned draft 2020-12 fixture.
// These are non-coercing checks; invalid cases must reach the expected target,
// not appear to pass the comparison because the reference was unresolved.
func TestSourceV160SchemaExternalReferences(t *testing.T) {
	const fixture = "tests/testdata/upstream-main/ref-remote-2020-12-v160/"
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(fixture + path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var groups []struct {
		Description string
		Schema      map[string]any
		Tests       []struct {
			Description string
			Data        any
			Valid       bool
		}
	}
	if err := json.Unmarshal(read("refRemote.json"), &groups); err != nil {
		t.Fatal(err)
	}
	const base = "http://localhost:1234/draft2020-12/"
	const expectedInteger = "root: expected integer, got string"
	const expectedString = "root: expected string, got integer"
	selections := []struct {
		group     string
		documents []string
		rejection string
		cases     int
	}{
		{"remote ref", []string{"integer.json"}, expectedInteger, 2},
		{"fragment within remote ref", []string{"subSchemas.json"}, expectedInteger, 2},
		{"anchor within remote ref", []string{"locationIndependentIdentifier.json"}, expectedInteger, 2},
		{"ref within remote ref", []string{"subSchemas.json"}, expectedInteger, 2},
		{"base URI change", []string{"baseUriChange/folderInteger.json"}, "0.0: expected integer, got string", 2},
		{"base URI change - change folder", []string{"baseUriChangeFolder/folderInteger.json"}, "list.0: expected integer, got string", 2},
		{"base URI change - change folder in subschema", []string{"baseUriChangeFolderInSubschema/folderInteger.json"}, "list.0: expected integer, got string", 2},
		{"root ref in remote ref", []string{"name-defs.json"}, "name: expected null, got object", 3},
		{"remote ref with ref to defs", []string{"ref-and-defs.json"}, "bar: expected string, got integer", 2},
		{"Location-independent identifier in remote ref", []string{"locationIndependentIdentifier.json"}, expectedInteger, 2},
		{"retrieved nested refs resolve relative to their URI not $id", []string{"nested/foo-ref-string.json", "nested/string.json"}, "name.foo: expected string, got integer", 2},
		{"remote HTTP ref with different $id", []string{"different-id-ref-string.json"}, expectedString, 2},
		{"remote HTTP ref with different URN $id", []string{"urn-ref-string.json"}, expectedString, 2},
		{"remote HTTP ref with nested absolute ref", []string{"nested-absolute-ref-to-string.json"}, expectedString, 2},
		{"$ref to $ref finds detached $anchor", []string{"detached-ref.json"}, expectedInteger, 2},
	}
	if len(groups) != len(selections) {
		t.Fatalf("pinned fixture contains %d groups, want %d", len(groups), len(selections))
	}
	for i, selection := range selections {
		group := groups[i]
		invalid := 0
		for _, c := range group.Tests {
			if !c.Valid {
				invalid++
			}
		}
		if group.Description != selection.group || len(group.Tests) != selection.cases || invalid != 1 {
			t.Fatalf("unexpected pinned group %d: %+v", i, group)
		}
		resources := make(map[string]json.RawMessage, len(selection.documents))
		for _, document := range selection.documents {
			resources[base+document] = read("remotes/" + document)
		}
		t.Run(group.Description, func(t *testing.T) {
			for _, c := range group.Tests {
				t.Run(c.Description, func(t *testing.T) {
					err := goai.ValidateJSONSchemaValueWithResources(group.Schema, c.Data, resources)
					if (err == nil) != c.Valid {
						t.Fatalf("validation = %v; want valid=%t", err, c.Valid)
					}
					if !c.Valid && err.Error() != selection.rejection {
						t.Fatalf("validation = %q; want %q", err, selection.rejection)
					}
				})
			}
		})
	}
}

func TestSchemaExternalReferenceOwnership(t *testing.T) {
	const uri = "https://schemas.example.invalid/property"
	schema := map[string]any{"$ref": uri}
	value := map[string]any{"prop": "kept"}
	stringResource := json.RawMessage(`{"properties":{"prop":{"type":"string"}}}`)
	resources := map[string]json.RawMessage{uri + "#alias": stringResource}
	if err := goai.ValidateJSONSchemaValueWithResources(schema, value, resources); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schema, map[string]any{"$ref": uri}) || !reflect.DeepEqual(value, map[string]any{"prop": "kept"}) || len(resources) != 1 || string(resources[uri+"#alias"]) != `{"properties":{"prop":{"type":"string"}}}` {
		t.Fatal("validation mutated host inputs")
	}
	if err := goai.ValidateJSONSchemaValueWithResources(schema, value, map[string]json.RawMessage{uri: json.RawMessage(`{"properties":{"prop":{"type":"integer"}}}`)}); err == nil || err.Error() != "prop: expected integer, got string" {
		t.Fatalf("independent registry validation = %v", err)
	}
	if err := goai.ValidateJSONSchemaValueWithResources(schema, value, nil); err == nil || err.Error() != "root: unresolved reference "+uri {
		t.Fatalf("unregistered document validation = %v", err)
	}
	if goai.CheckJSONSchemaValue(schema, value) {
		t.Fatal("default validator reused an external registration")
	}
}

// Supplemental API checks: registry configuration errors are distinct from
// rejecting a well-formed value against a supplied document.
func TestSchemaExternalReferenceRegistration(t *testing.T) {
	const uri = "https://schemas.example.invalid/schema"
	schema := map[string]any{"$ref": uri}
	for _, test := range []struct {
		name      string
		resources map[string]json.RawMessage
		want      string
	}{
		{"true document", map[string]json.RawMessage{uri: json.RawMessage(`true`)}, ""},
		{"non-schema document", map[string]json.RawMessage{uri: json.RawMessage(`null`)}, `schema resource "` + uri + `": expected an object or boolean`},
		{"colliding aliases", map[string]json.RawMessage{uri: json.RawMessage(`true`), uri + "#alias": json.RawMessage(`false`)}, `duplicate schema resource for "` + uri + `"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var got string
			if err := goai.ValidateJSONSchemaValueWithResources(schema, "value", test.resources); err != nil {
				got = err.Error()
			}
			if got != test.want {
				t.Fatalf("validation = %q; want %q", got, test.want)
			}
		})
	}
}
