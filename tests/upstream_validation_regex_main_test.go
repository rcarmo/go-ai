package goai_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Exact matrix snapshot 503 and the 32 source-derived controls preserved in
// 8712b7e, promoted unchanged now that ECMAScript Unicode-mode checks pass.
func TestUpstreamMainValidationRegexFormats(t *testing.T) {
	mainRunFormatFixture(t, "validation-regex.json", 1, 32)
}

func TestUpstreamMainValidationRegexDependency(t *testing.T) {
	var f struct {
		Upstream   string
		Dependency []struct {
			Name          string
			Schema, Input json.RawMessage
			Valid         bool
		}
	}
	data, err := os.ReadFile("testdata/upstream-main/validation-regex.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.Dependency) != 18 {
		t.Fatal("unexpected pin/count")
	}
	for _, tc := range f.Dependency {
		t.Run(tc.Name, func(t *testing.T) {
			// Original format-only schema/input/validity; no added value type.
			// This public wrapper establishes acceptance, not output equivalence
			// for optional null (the general null-retention gap stays open).
			schema := `{"type":"object","properties":{"v":` + string(tc.Schema) + `}}`
			input := `{"v":` + string(tc.Input) + `}`
			got := mainNumericOutcome(t, schema, input)
			if strings.HasPrefix(got, "ok ") != tc.Valid {
				t.Fatalf("got %s valid=%v", got, tc.Valid)
			}
		})
	}
}
