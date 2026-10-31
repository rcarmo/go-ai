package goai_test

import (
	"bytes"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"os"
	"reflect"
	"sort"
	"testing"
)

// JSON Schema required is a set; retain native deterministic ordering while
// comparing its meaning with the pinned JavaScript object's insertion order.
func strictSchemaMeaning(raw json.RawMessage) any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		panic(err)
	}
	var normalize func(any)
	normalize = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			for key, child := range node {
				if key == "required" {
					if names, ok := child.([]any); ok {
						sort.Slice(names, func(i, j int) bool { return names[i].(string) < names[j].(string) })
					}
				}
				normalize(child)
			}
		case []any:
			for _, child := range node {
				normalize(child)
			}
		}
	}
	normalize(value)
	return value
}

func TestPinnedStrictSchemaVectorsMain(t *testing.T) {
	raw, err := os.ReadFile("../testdata/strict_schema_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Upstream string
		Vectors  []struct {
			Name          string
			Input, Output json.RawMessage
			Error         string
			Sampling      []struct {
				Supported bool
				Strict    string
				Value     *bool
				Error     bool
			}
		}
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Upstream != "4b535d57ea8863dbba44b89872146523da271786" || len(fixture.Vectors) != 33 {
		t.Fatal("fixture provenance/count", fixture.Upstream, len(fixture.Vectors))
	}
	for _, tc := range fixture.Vectors {
		t.Run(tc.Name, func(t *testing.T) {
			before := append([]byte(nil), tc.Input...)
			output, err := goai.MakeStrictJSONSchema(tc.Input)
			if tc.Error != "" {
				if err == nil || err.Error() != tc.Error {
					t.Fatalf("strict rejection=%v want %q", err, tc.Error)
				}
			} else if err != nil || !reflect.DeepEqual(strictSchemaMeaning(output), strictSchemaMeaning(tc.Output)) {
				t.Fatalf("strict result=%s err=%v want=%s", output, err, tc.Output)
			}
			for _, sampling := range tc.Sampling {
				tool := goai.Tool{Name: "sample", Parameters: tc.Input, ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: sampling.Strict}}
				value, err := goai.ResolveJSONSchemaStrictSampling(tool, sampling.Supported)
				if (err != nil) != sampling.Error || !reflect.DeepEqual(value, sampling.Value) {
					t.Errorf("sampling supported=%t strict=%s: %v/%v want %v/error=%t", sampling.Supported, sampling.Strict, value, err, sampling.Value, sampling.Error)
				}
			}
			if !bytes.Equal(before, tc.Input) {
				t.Fatal("strict conversion changed caller JSON")
			}
		})
	}
}
