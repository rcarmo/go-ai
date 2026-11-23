package goai_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Run the pinned dynamicRef.json vectors through two strict calls. The source
// conformance runner checks validity and error agreement; the additional exact
// records were captured from the pinned Validator.errors without modifying it.
func TestSourceV160DynamicReferenceDiagnostics(t *testing.T) {
	const fixture = "tests/testdata/upstream-main/dynamic-ref-2020-12-v160"
	read := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var groups []struct {
		Description string
		Schema      json.RawMessage
		Tests       []struct {
			Description string
			Data        any
			Valid       bool
		}
	}
	if err := json.Unmarshal(read(fixture+".json"), &groups); err != nil {
		t.Fatal(err)
	}
	const base = "http://localhost:1234/draft2020-12/"
	documents := []string{"tree.json", "extendible-dynamic-ref.json", "extendible-dynamic-ref.json", "extendible-dynamic-ref.json", "detached-dynamicref.json"}
	var registries [5]map[string]json.RawMessage
	for i, name := range documents {
		registries[i] = map[string]json.RawMessage{base + name: read(fixture + "-remotes/" + name)}
	}

	invalid := []goai.JSONSchemaDiagnostic{{Path: "", Keyword: "schema", Message: "invalid"}}
	dynamic := func(paths ...string) []goai.JSONSchemaDiagnostic {
		issues := make([]goai.JSONSchemaDiagnostic, 0, len(paths))
		for _, path := range paths {
			issues = append(issues, goai.JSONSchemaDiagnostic{Path: path, Keyword: "$dynamicRef", Message: "does not match the dynamically referenced schema"})
		}
		return issues
	}
	extendible := []goai.JSONSchemaDiagnostic{
		{Path: "/elements", Keyword: "required", Message: "missing"},
		{Path: "/a", Keyword: "additionalProperties", Message: "unknown field"},
	}
	invalidCases := map[[2]int][]goai.JSONSchemaDiagnostic{
		{0, 1}:  dynamic("/1"),
		{1, 1}:  dynamic("/1"),
		{2, 1}:  {{Path: "/1", Keyword: "type", Message: "expected string, got integer"}},
		{3, 1}:  invalid,
		{4, 0}:  dynamic("/0", "/1"),
		{5, 1}:  invalid,
		{9, 1}:  invalid,
		{11, 1}: invalid,
		{11, 2}: invalid,
		{12, 0}: invalid,
		{12, 1}: dynamic(""),
		{13, 0}: invalid,
		{14, 0}: extendible,
		{14, 1}: invalid,
		{15, 0}: extendible,
		{15, 1}: invalid,
		{16, 0}: extendible,
		{16, 1}: invalid,
		{17, 1}: dynamic(""),
		{18, 1}: dynamic("/false"),
		{19, 1}: dynamic("/bar-item/content"),
		{20, 1}: invalid,
	}
	if len(groups) != 21 {
		t.Fatalf("pinned fixture groups = %d; want 21", len(groups))
	}
	count, rejected := 0, 0
	for g, group := range groups {
		for v, c := range group.Tests {
			count++
			_, invalidCase := invalidCases[[2]int{g, v}]
			if invalidCase {
				rejected++
			}
			if invalidCase == c.Valid {
				t.Fatalf("fixture group %d vector %d: captured rejection=%t, fixture validity=%t", g, v, invalidCase, c.Valid)
			}
		}
	}
	if count != 44 || rejected != len(invalidCases) {
		t.Fatalf("vectors/rejections = %d/%d; want 44/%d", count, rejected, len(invalidCases))
	}

	for g, group := range groups {
		var schema map[string]any
		if err := json.Unmarshal(group.Schema, &schema); err != nil {
			t.Fatal(err)
		}
		var resources map[string]json.RawMessage
		if g >= 13 && g <= 17 {
			resources = registries[g-13]
		}
		t.Run(group.Description, func(t *testing.T) {
			for v, c := range group.Tests {
				want := invalidCases[[2]int{g, v}]
				if c.Valid {
					want = []goai.JSONSchemaDiagnostic{}
				}
				t.Run(c.Description, func(t *testing.T) {
					err := goai.ValidateJSONSchemaValueWithResources(schema, c.Data, resources)
					if (err == nil) != c.Valid {
						t.Fatalf("group %d vector %d: validation = %v; want valid=%t", g, v, err, c.Valid)
					}
					issues, explainErr := goai.ExplainJSONSchemaValueWithResources(group.Schema, c.Data, resources)
					if explainErr != nil {
						t.Fatal(explainErr)
					}
					if (len(issues) == 0) != (err == nil) {
						t.Fatalf("group %d vector %d: diagnostics = %+v; disagree with validation %v", g, v, issues, err)
					}
					if !reflect.DeepEqual(issues, want) {
						t.Fatalf("group %d vector %d: diagnostics = %+v; want %+v", g, v, issues, want)
					}
				})
			}
		})
	}
}
