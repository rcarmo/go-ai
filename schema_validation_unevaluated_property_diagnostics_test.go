package goai_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Supplemental API checks for nested object order and the unchanged first
// model-facing diagnostic, even when the raw input carries ordering metadata.
func TestSchemaUnevaluatedPropertiesInstanceOrder(t *testing.T) {
	const schema = `{"type":"array","items":{"unevaluatedProperties":false}}`
	const input = `[{"z":1,"14":2,"2":3,"a":4}]`
	value := decodeV160DiagnosticInput(t, input)
	for _, tc := range []struct {
		name string
		data any
		want []string
	}{
		{"raw object order", json.RawMessage(input), []string{"/0/2", "/0/14", "/0/z", "/0/a"}},
		{"decoded map fallback", value, []string{"/0/2", "/0/14", "/0/a", "/0/z"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := make([]goai.JSONSchemaDiagnostic, 0, len(tc.want))
			for _, path := range tc.want {
				want = append(want, goai.JSONSchemaDiagnostic{Path: path, Keyword: "unevaluatedProperties", Message: "unknown field"})
			}
			issues, err := goai.ExplainJSONSchemaValueWithResources(json.RawMessage(schema), tc.data, nil)
			if err != nil || !reflect.DeepEqual(issues, want) {
				t.Fatalf("diagnostics = %+v, %v; want %+v", issues, err, want)
			}
		})
	}
	tool := goai.Tool{Name: "unevaluated-properties", Parameters: json.RawMessage(schema)}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: value})
	if got != nil || err == nil {
		t.Fatalf("tool result = %#v, %v; want rejection", got, err)
	}
	assertV160DiagnosticLines(t, err, tool.Name, "  - 0.14: unknown field")

	issues, err := goai.ExplainJSONSchemaValueWithResources(json.RawMessage(schema), json.RawMessage(`{`), nil)
	if issues != nil || err == nil || err.Error() != "decode value: unexpected end of JSON input" {
		t.Fatalf("malformed input = %+v, %v; want decode error", issues, err)
	}
}

// The pinned conformance runner compares error-list emptiness with check.
// The supplemental pinned Validator.errors capture also fixes each path,
// keyword, message, and position within the array.
func TestSourceV160SchemaUnevaluatedPropertiesAgreement(t *testing.T) {
	fixture, err := os.ReadFile("tests/testdata/upstream-main/unevaluated-properties-2020-12-v160.json")
	if err != nil {
		t.Fatal(err)
	}
	var groups []struct {
		Description string
		Schema      json.RawMessage
		Tests       []struct {
			Description string
			Data        json.RawMessage
			Valid       bool
		}
	}
	if err := json.Unmarshal(fixture, &groups); err != nil {
		t.Fatal(err)
	}
	diagnostics := func(keyword, message string, paths ...string) []goai.JSONSchemaDiagnostic {
		out := make([]goai.JSONSchemaDiagnostic, 0, len(paths))
		for _, path := range paths {
			out = append(out, goai.JSONSchemaDiagnostic{Path: path, Keyword: keyword, Message: message})
		}
		return out
	}
	unknown := func(paths ...string) []goai.JSONSchemaDiagnostic {
		return diagnostics("unevaluatedProperties", "unknown field", paths...)
	}
	invalidCases := map[[2]int][]goai.JSONSchemaDiagnostic{
		{1, 2}:   diagnostics("minLength", "expected at least 3 characters", "/foo"),
		{2, 1}:   unknown("/foo"),
		{3, 1}:   unknown("/bar"),
		{4, 1}:   unknown("/bar"),
		{7, 1}:   unknown("/baz"),
		{8, 1}:   unknown("/baz"),
		{11, 1}:  unknown("/baz"),
		{11, 3}:  unknown("/quux"),
		{12, 1}:  unknown("/quux"),
		{13, 0}:  unknown("/bar"),
		{14, 1}:  unknown("/baz"),
		{14, 3}:  unknown("/foo"),
		{15, 1}:  unknown("/bar"),
		{15, 3}:  unknown("/foo"),
		{16, 1}:  unknown("/baz"),
		{16, 2}:  unknown("/baz"),
		{16, 3}:  unknown("/foo", "/baz"),
		{17, 1}:  unknown("/bar"),
		{18, 1}:  unknown("/bar"),
		{19, 1}:  unknown("/baz"),
		{20, 1}:  unknown("/baz"),
		{21, 1}:  unknown("/bar", "/baz"),
		{22, 0}:  unknown("/foo"),
		{23, 0}:  unknown("/foo"),
		{26, 0}:  unknown("/foo"),
		{26, 1}:  unknown("/foo", "/bar"),
		{27, 1}:  unknown("/bar"),
		{28, 0}:  unknown("/foo"),
		{28, 1}:  unknown("/foo", "/bar"),
		{29, 1}:  unknown("/bar"),
		{30, 1}:  unknown("/foo/faz"),
		{31, 0}:  unknown("/bar"),
		{31, 2}:  unknown("/bar"),
		{32, 0}:  unknown("/foo"),
		{32, 1}:  unknown("/foo"),
		{33, 2}:  unknown("/y"),
		{33, 4}:  unknown("/x/y"),
		{33, 6}:  unknown("/x/x/y"),
		{34, 0}:  diagnostics("required", "missing", "/x"),
		{34, 1}:  diagnostics("required", "missing", "/x"),
		{34, 2}:  diagnostics("oneOf", "matches more than one alternative", ""),
		{34, 7}:  diagnostics("oneOf", "matches more than one alternative", ""),
		{35, 0}:  diagnostics("required", "missing", "/all"),
		{35, 5}:  diagnostics("oneOf", "matches more than one alternative", ""),
		{35, 6}:  diagnostics("oneOf", "matches more than one alternative", ""),
		{35, 7}:  diagnostics("oneOf", "matches more than one alternative", ""),
		{35, 8}:  diagnostics("required", "missing", "/a"),
		{35, 9}:  diagnostics("required", "missing", "/a"),
		{35, 10}: diagnostics("required", "missing", "/all"),
		{35, 13}: unknown("/foo"),
		{35, 14}: diagnostics("oneOf", "matches more than one alternative", ""),
		{35, 15}: diagnostics("required", "missing", "/a"),
		{35, 16}: diagnostics("required", "missing", "/a"),
		{35, 17}: diagnostics("required", "missing", "/a"),
		{35, 20}: diagnostics("oneOf", "matches more than one alternative", ""),
		{38, 1}:  diagnostics("type", "expected number, got string", "/a"),
		{39, 1}:  unknown("/bar"),
		{40, 0}:  unknown("/foo"),
		{40, 1}:  unknown("/bar"),
		{41, 0}:  unknown("/bar"),
		{42, 1}:  unknown("/bar"),
		{43, 1}:  unknown("/bar"),
	}
	if len(groups) != 44 {
		t.Fatalf("fixture groups = %d; want 44", len(groups))
	}
	count, rejected := 0, 0
	for g, group := range groups {
		for v, c := range group.Tests {
			count++
			_, invalid := invalidCases[[2]int{g, v}]
			if invalid {
				rejected++
			}
			if invalid == c.Valid {
				t.Fatalf("group %d vector %d: captured rejection=%t, fixture validity=%t", g, v, invalid, c.Valid)
			}
		}
	}
	if count != 129 || rejected != 62 || rejected != len(invalidCases) {
		t.Fatalf("fixture vectors/rejections = %d/%d; want 129/62", count, rejected)
	}
	for g, group := range groups {
		var schema map[string]any
		if err := json.Unmarshal(group.Schema, &schema); err != nil {
			t.Fatal(err)
		}
		t.Run(group.Description, func(t *testing.T) {
			for v, c := range group.Tests {
				want := invalidCases[[2]int{g, v}]
				if c.Valid {
					want = []goai.JSONSchemaDiagnostic{}
				}
				t.Run(c.Description, func(t *testing.T) {
					value := decodeV160DiagnosticInput(t, string(c.Data))
					check := goai.CheckJSONSchemaValue(schema, value)
					if check != c.Valid {
						t.Fatalf("group %d vector %d: check = %t; want %t", g, v, check, c.Valid)
					}
					issues, err := goai.ExplainJSONSchemaValueWithResources(group.Schema, c.Data, nil)
					if err != nil {
						t.Fatal(err)
					}
					if (len(issues) == 0) != check {
						t.Fatalf("group %d vector %d: diagnostics = %+v; disagree with check %t", g, v, issues, check)
					}
					if !reflect.DeepEqual(issues, want) {
						t.Fatalf("group %d vector %d: diagnostics = %+v; want %+v", g, v, issues, want)
					}
				})
			}
		})
	}
}
