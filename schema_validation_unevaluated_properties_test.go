package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV160SchemaUnevaluatedProperties(t *testing.T) {
	checkV160SchemaAnnotationFixture(t, "unevaluated-properties-2020-12-v160.json", 44, 129)
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
