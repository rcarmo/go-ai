package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Supplemental results from the pinned predicate. Unlike the folded URI and
// address formats, duration markers must have their original case.
func TestSchemaDurationFormatCaseAndSyntax(t *testing.T) {
	schema := json.RawMessage(`{"format":"duration"}`)
	options := goai.JSONSchemaCheckOptions{AssertFormats: true}
	compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{`"P1D"`, true}, {`"p1d"`, false}, {`"P1d"`, false},
		{`"PT1S"`, true}, {`"Pt1S"`, false}, {`"P١D"`, false},
		{`"PT1.5S"`, false}, {`"P1D\n"`, false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			assertV160PreparedSchemaCheck(t, compiled, tc.input, tc.want)
			if got, err := goai.CheckJSONSchemaValueWithOptions(schema, json.RawMessage(tc.input), options); err != nil || got != tc.want {
				t.Fatalf("standalone check(%s) = %t, %v; want %t", tc.input, got, err, tc.want)
			}
			want := `[]`
			if !tc.want {
				want = `[{"path":"","keyword":"format","message":"expected format duration"}]`
			}
			assertV160PreparedMessageRecords(t, compiled, tc.input, want)
		})
	}
}
