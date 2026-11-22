package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV160SchemaUnevaluatedProperties(t *testing.T) {
	checkV160SchemaAnnotationFixture(t, "unevaluated-properties-2020-12-v160.json", 44, 129)
}

// The pinned explainer reports exactly one error for each rejected value in
// unevaluatedProperties.json groups 19 and 20: /baz, unevaluatedProperties,
// "unknown field". The native projection is the first error's path and message;
// it has neither a keyword field nor the full list available from compile().errors().
func TestSourceV160SchemaUnevaluatedPropertiesFirstDiagnostic(t *testing.T) {
	groups := readV160SchemaAnnotationFixture(t, "unevaluated-properties-2020-12-v160.json")
	for _, group := range []struct {
		index int
		name  string
	}{
		{19, "unevaluatedProperties with $ref"},
		{20, "unevaluatedProperties before $ref"},
	} {
		t.Run(group.name, func(t *testing.T) {
			checkV160UnevaluatedPropertyDiagnosticPair(t, groups[group.index], group.name, "baz: unknown field")
		})
	}
}

// Supplemental, deliberately not an exact upstream diagnostic mapping. In
// group 21 the pinned explainer additionally reports /bar for the invalid
// value, even though the equivalent value without /baz passes its checker.
func TestSchemaUnevaluatedPropertiesDynamicDiagnostic(t *testing.T) {
	groups := readV160SchemaAnnotationFixture(t, "unevaluated-properties-2020-12-v160.json")
	checkV160UnevaluatedPropertyDiagnosticPair(t, groups[21], "unevaluatedProperties with $dynamicRef", "baz: unknown field")
}

func checkV160UnevaluatedPropertyDiagnosticPair(t *testing.T, group v160SchemaAnnotationGroup, wantGroup, wantError string) {
	t.Helper()
	if group.Description != wantGroup || len(group.Tests) != 2 || !group.Tests[0].Valid || group.Tests[1].Valid {
		t.Fatalf("unexpected source group %q: %+v", group.Description, group.Tests)
	}
	for i, c := range group.Tests {
		t.Run(c.Description, func(t *testing.T) {
			if got := goai.CheckJSONSchemaValue(group.Schema, c.Data); got != c.Valid {
				t.Errorf("check = %t; want %t", got, c.Valid)
			}
			// An exact error string detects a generic fallback or the wrong
			// blamed field, even when the membership verdict is unchanged.
			got := goai.ValidateJSONSchemaValueWithResources(group.Schema, c.Data, nil)
			if i == 0 {
				if got != nil {
					t.Errorf("validation = %v; want no error", got)
				}
			} else if got == nil || got.Error() != wantError {
				t.Errorf("first diagnostic = %v; want %q", got, wantError)
			}
		})
	}
}

// Supplemental native diagnostic paths; not an independent errors oracle.
func TestSchemaUnevaluatedPropertiesDiagnostics(t *testing.T) {
	for _, c := range []struct{ name, schema, data, want string }{
		{"discard failed branch", `{"anyOf":[{"properties":{"x":{"const":1}}},true],"unevaluatedProperties":false}`, `{"x":2}`, "x: unknown field"},
		{"branch isolation", `{"allOf":[{"properties":{"x":true}},{"unevaluatedProperties":false}]}`, `{"x":1}`, "x: unknown field"},
		{"nested path", `{"properties":{"child":{"unevaluatedProperties":false}}}`, `{"child":{"x":1}}`, "child.x: unknown field"},
		{"target failure", `{"properties":{"owned":true},"unevaluatedProperties":{"type":"string"}}`, `{"owned":1,"extra":1}`, "extra: expected string, got integer"},
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
