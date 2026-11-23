package goai_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// The model error format retains the first rejected leftover. The standalone
// explanation now reports all leftover indexes from the same schema and value.
func TestSchemaUnevaluatedItemsToolFirstDiagnostic(t *testing.T) {
	const schema = `{"type":"array","prefixItems":[true],"unevaluatedItems":false}`
	value := decodeV160DiagnosticInput(t, `[0,1,2]`)
	tool := goai.Tool{Name: "unevaluated-items", Parameters: json.RawMessage(schema)}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: value})
	if got != nil || err == nil {
		t.Fatalf("tool result = %#v, %v; want rejection", got, err)
	}
	assertV160DiagnosticLines(t, err, tool.Name, "  - 1: no value is allowed here")
	issues, err := goai.ExplainJSONSchemaValueWithResources(json.RawMessage(schema), value, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, issues, `[{"path":"/1","keyword":"unevaluatedItems","message":"unexpected item"},{"path":"/2","keyword":"unevaluatedItems","message":"unexpected item"}]`)
}

// The pinned conformance runner compares error-list emptiness with check.
// The supplemental pinned Validator.errors capture also fixes each path,
// keyword, message, and position within the array.
func TestSourceV160SchemaUnevaluatedItemsAgreement(t *testing.T) {
	fixture, err := os.ReadFile("tests/testdata/upstream-main/unevaluated-items-2020-12-v160.json")
	if err != nil {
		t.Fatal(err)
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
	unexpected := func(paths ...string) []goai.JSONSchemaDiagnostic {
		return diagnostics("unevaluatedItems", "unexpected item", paths...)
	}
	invalidCases := map[[2]int][]goai.JSONSchemaDiagnostic{
		{1, 1}:  unexpected("/0"),
		{2, 2}:  diagnostics("type", "expected string, got integer", "/0"),
		{4, 1}:  unexpected("/1"),
		{6, 1}:  diagnostics("type", "expected number, got string", "/0", "/1", "/2"),
		{7, 1}:  unexpected("/2"),
		{8, 2}:  diagnostics("type", "expected boolean, got string", "/0"),
		{11, 1}: unexpected("/2"),
		{11, 3}: unexpected("/3"),
		{12, 1}: unexpected("/2"),
		{13, 0}: unexpected("/1"),
		{14, 1}: unexpected("/3"),
		{14, 3}: unexpected("/4"),
		{15, 1}: unexpected("/0"),
		{16, 1}: unexpected("/2"),
		{17, 1}: unexpected("/2"),
		{18, 1}: unexpected("/1", "/2"),
		{19, 0}: unexpected("/0"),
		{20, 1}: unexpected("/foo/1"),
		{21, 1}: diagnostics("contains", "expected at least 1 matching item", ""),
		{21, 2}: unexpected("/1"),
		{22, 1}: diagnostics("multipleOf", "expected a multiple of 5", "/3"),
		{23, 4}: unexpected("/0", "/1"),
		{23, 5}: unexpected("/0", "/1"),
		{23, 6}: unexpected("/0", "/1", "/2", "/3", "/4"),
		{23, 7}: unexpected("/0", "/2", "/4"),
		{24, 1}: unexpected("/0"),
		{24, 2}: unexpected("/1"),
		{27, 1}: unexpected("/0"),
		{28, 0}: unexpected("/1"),
	}
	if len(groups) != 29 {
		t.Fatalf("fixture groups = %d; want 29", len(groups))
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
	if count != 71 || rejected != 29 || rejected != len(invalidCases) {
		t.Fatalf("fixture vectors/rejections = %d/%d; want 71/29", count, rejected)
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
					check := goai.CheckJSONSchemaValue(schema, c.Data)
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
