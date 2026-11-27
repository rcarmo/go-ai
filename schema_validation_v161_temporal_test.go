package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161SchemaPreparedTemporalFormatConformance(t *testing.T) {
	checkV161SchemaPreparedFormatConformance(t, preparedFormatFixture{"date", 81}, preparedFormatFixture{"time", 56}, preparedFormatFixture{"date-time", 47})
}

// Supplemental results from the pinned predicates. Date does not accept a
// timestamp; time and date-time require zones and normalize leap seconds to UTC
// for the check only, without modifying input strings.
func TestSchemaPreparedTemporalFormatBoundaries(t *testing.T) {
	for _, tc := range []struct {
		format string
		pass   []string
		fail   []string
	}{
		{"date", []string{"0000-02-29", "2000-02-29"}, []string{"1900-02-29", "2000-02-29T12:00:00Z"}},
		{"time", []string{"23:59:60.5Z", "00:59:60+01:00", "12:00:00+23:59"}, []string{"00:58:60+01:00", "23:59:59", "12:00:00+24:00"}},
		{"date-time", []string{"2000-02-29t00:59:60.5+01:00", "2000-02-29T12:00:00z"}, []string{"2000-02-29T12:00:00", "2000-02-29 12:00:00Z"}},
	} {
		t.Run(tc.format, func(t *testing.T) {
			schema := json.RawMessage(`{"type":"string","format":"` + tc.format + `"}`)
			compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{AssertFormats: true}})
			if err != nil {
				t.Fatal(err)
			}
			for _, input := range tc.pass {
				got, issues, err := compiled.Parse(input)
				if err != nil || got != input {
					t.Fatalf("parse(%q) = %#v, %v; want the input unchanged", input, got, err)
				}
				assertV160StructuredDiagnostics(t, issues, `[]`)
			}
			for _, input := range tc.fail {
				got, issues, err := compiled.Parse(input)
				if err != nil || got != nil {
					t.Fatalf("parse(%q) = %#v, %v; want a format diagnostic", input, got, err)
				}
				assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"format","message":"expected format `+tc.format+`"}]`)
			}
		})
	}
}
