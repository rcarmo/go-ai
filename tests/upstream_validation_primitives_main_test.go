package goai_test

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestUpstreamMainValidationPrimitives(t *testing.T) {
	type row struct {
		Name                    string
		Schema, Input, Expected json.RawMessage
		Valid                   bool
	}
	var f struct {
		Upstream                               string
		Literal, Dependency, Rejected, Derived []row
		NativeBigInt                           []struct {
			Name             string
			Schema, Expected json.RawMessage
			Input            string
		}
	}
	data, err := os.ReadFile("testdata/upstream-main/validation-primitives.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	if f.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(f.Literal) != 4 || len(f.Dependency) != 65 || len(f.Rejected) != 20 || len(f.Derived) != 40 || len(f.NativeBigInt) != 5 {
		t.Fatal("unexpected pinned primitive fixture")
	}
	for _, group := range []struct {
		name string
		rows []row
	}{{"literal", f.Literal}, {"dependency", f.Dependency}, {"public-rejection", f.Rejected}, {"source-derived", f.Derived}} {
		t.Run(group.name, func(t *testing.T) {
			for _, tc := range group.rows {
				t.Run(tc.Name, func(t *testing.T) {
					t.Parallel()
					got, args, err := mainValidateJSON(t, string(tc.Schema), string(tc.Input))
					mainAssertValidationJSON(t, args, string(tc.Input))
					if (err == nil) != tc.Valid {
						t.Fatalf("valid=%v want %v; error=%v", err == nil, tc.Valid, err)
					}
					if err == nil {
						mainAssertValidationJSON(t, got, string(tc.Expected))
					}
				})
			}
		})
	}
	for _, tc := range f.NativeBigInt {
		t.Run(tc.Name+"-int64", func(t *testing.T) {
			value, err := strconv.ParseInt(tc.Input, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			tool := goai.Tool{Name: "primitive", Parameters: tc.Schema}
			args := map[string]interface{}{"v": value}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			mainAssertValidationJSON(t, got, string(tc.Expected))
			if args["v"] != value {
				t.Fatal("input changed")
			}
		})
	}
}

func TestUpstreamMainValidationPrimitivesNativePrecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    interface{}
		expected interface{}
	}{
		{"int64", int64(math.MaxInt64), int64(math.MaxInt64)},
		{"integer-decimal", json.Number("9007199254740993"), json.Number("9007199254740993")},
		{"fractional-decimal", json.Number("9007199254740993.99"), json.Number("9007199254740993")},
		{"negative-decimal", json.Number("-9007199254740993.99"), json.Number("-9007199254740993")},
		{"tiny-decimal", json.Number("-0.0000001"), json.Number("-0")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := goai.Tool{Name: "integer", Parameters: json.RawMessage(`{"type":"object","properties":{"v":{"type":"integer"}},"required":["v"]}`)}
			args := map[string]interface{}{"v": tc.value}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if got["v"] != tc.expected {
				t.Fatalf("got %T %v, want %T %v", got["v"], got["v"], tc.expected, tc.expected)
			}
			if args["v"] != tc.value {
				t.Fatal("input changed")
			}
		})
	}
}

func TestUpstreamMainValidationPrimitivesSignedZero(t *testing.T) {
	for _, kind := range []string{"integer", "number"} {
		t.Run(kind, func(t *testing.T) {
			tool := goai.Tool{Name: "zero", Parameters: json.RawMessage(`{"type":"object","properties":{"v":{"type":"` + kind + `"}},"required":["v"]}`)}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"v": "-0"}})
			if err != nil {
				t.Fatal(err)
			}
			value, ok := got["v"].(float64)
			if !ok || value != 0 || !math.Signbit(value) {
				t.Fatalf("negative zero lost: %T %v", got["v"], got["v"])
			}
		})
	}
}

func TestUpstreamMainValidationPrimitivesNonfiniteInteger(t *testing.T) {
	tool := goai.Tool{Name: "integer", Parameters: json.RawMessage(`{"type":"object","properties":{"v":{"type":"integer"}},"required":["v"]}`)}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"v": value}}); err == nil {
			t.Fatalf("non-integer accepted: %#v", got)
		}
	}
}
