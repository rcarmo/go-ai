package goai_test

import (
	"encoding/json"
	"os"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Byte-identical draft 2020-12 JSON Schema Test Suite fixture from pi v1.6.0.
// CheckJSONSchemaValue corresponds to the non-coercing source check directly.
func TestSourceV160SchemaUnevaluatedItems(t *testing.T) {
	checkV160SchemaAnnotationFixture(t, "unevaluated-items-2020-12-v160.json", 29, 71)
}

func checkV160SchemaAnnotationFixture(t *testing.T, file string, wantGroups, wantCases int) {
	t.Helper()
	fixture, err := os.ReadFile("tests/testdata/upstream-main/" + file)
	if err != nil {
		t.Fatal(err)
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
	if err := json.Unmarshal(fixture, &groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != wantGroups {
		t.Fatalf("got %d source groups, want %d", len(groups), wantGroups)
	}
	cases := 0
	for _, group := range groups {
		cases += len(group.Tests)
		t.Run(group.Description, func(t *testing.T) {
			for _, c := range group.Tests {
				t.Run(c.Description, func(t *testing.T) {
					if got := goai.CheckJSONSchemaValue(group.Schema, c.Data); got != c.Valid {
						t.Fatalf("check = %t; want %t", got, c.Valid)
					}
				})
			}
		})
	}
	if cases != wantCases {
		t.Fatalf("got %d source cases, want %d", cases, wantCases)
	}
}

// Supplemental native diagnostics, not a translation of source error agreement.
func TestSchemaUnevaluatedItemsDiagnostics(t *testing.T) {
	for _, c := range []struct {
		name, schema, data, want string
	}{
		{"contributing indexes", `{"allOf":[{"contains":{"multipleOf":2}},{"contains":{"multipleOf":3}}],"unevaluatedItems":{"type":"string"}}`, `[2,3,4,7,8]`, "3: expected string, got integer"},
		{"branch isolation", `{"allOf":[{"prefixItems":[true]},{"unevaluatedItems":false}]}`, `[1]`, "0: no value is allowed here"},
		{"nested item path", `{"properties":{"foo":{"prefixItems":[true],"unevaluatedItems":false}}}`, `{"foo":[1,2]}`, "foo.1: no value is allowed here"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var schema map[string]any
			var data any
			if err := json.Unmarshal([]byte(c.schema), &schema); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(c.data), &data); err != nil {
				t.Fatal(err)
			}
			if err := goai.ValidateJSONSchemaValueWithResources(schema, data, nil); err == nil || err.Error() != c.want {
				t.Fatalf("validation = %v; want %q", err, c.want)
			}
		})
	}
}
