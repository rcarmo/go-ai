package goai_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Eight of the twelve literal formats cases at validation-probes.test.ts
// 128-152/495-506. Keep the complete original schema; temporal assertions are
// separate, and hostname/regex assertions are credited in their own tests.
func TestUpstreamMainValidationSelectedFormats(t *testing.T) {
	mainRunFormatFixture(t, "validation-formats.json", 8, 31)
}

// Two remaining matrix date-time cases plus the two lowercase/space cases at
// source 392-394/587-588. Other rows are explicitly source-derived controls.
func TestUpstreamMainValidationTemporalFormats(t *testing.T) {
	mainRunFormatFixture(t, "validation-temporal.json", 4, 52)
}

func TestUpstreamMainValidationRegexIntegration(t *testing.T) {
	mainRunFormatFixture(t, "validation-regex-integration.json", 0, 8)
}

func mainRunFormatFixture(t *testing.T, filename string, wantLiteral, wantDerived int) {
	t.Helper()
	type formatCase struct {
		Name   string
		Schema json.RawMessage
		Input  json.RawMessage
		Want   string
	}
	var fixture struct {
		Upstream string
		Schema   json.RawMessage
		Literal  []formatCase
		Derived  []formatCase
	}
	data, err := os.ReadFile("testdata/upstream-main/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(fixture.Literal) != wantLiteral || len(fixture.Derived) != wantDerived {
		t.Fatal("unexpected oracle pin or case count")
	}
	for _, group := range []struct {
		name  string
		cases []formatCase
	}{{"literal", fixture.Literal}, {"source-derived", fixture.Derived}} {
		t.Run(group.name, func(t *testing.T) {
			for _, tc := range group.cases {
				t.Run(tc.Name, func(t *testing.T) {
					t.Parallel()
					schema := tc.Schema
					if len(schema) == 0 {
						schema = fixture.Schema
					}
					// The shared outcome adapter calls public ValidateToolArguments
					// and independently verifies that caller arguments stay intact.
					got := mainNumericOutcome(t, string(schema), string(tc.Input))
					if strings.HasPrefix(got, "ok ") && strings.HasPrefix(tc.Want, "ok ") {
						// JSON HTML escapes differ between Go and JS; Tool values do not.
						var value interface{}
						if err := json.Unmarshal([]byte(got[3:]), &value); err != nil {
							t.Fatal(err)
						}
						mainAssertValidationJSON(t, value, tc.Want[3:])
					} else if got != tc.Want {
						t.Fatalf("got %s, want %s", got, tc.Want)
					}
				})
			}
		})
	}
}
