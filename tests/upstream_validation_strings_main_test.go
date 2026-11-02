package goai_test

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Public-path adaptations of pinned c9f4c7b9 assertions; not a conversion-only
// helper substitute. Scalar Value.Convert inputs live in required property v.
// Go maps have no key order, so compare successful JSON structurally.
func TestUpstreamMainValidationStrings(t *testing.T) {
	type row struct {
		Name          string
		Schema, Input json.RawMessage
		Want          string
	}
	var fixture struct {
		Upstream                     string
		Literal, Dependency, Derived []row
		NativeBigInt, NativeFloat    []struct{ Name, Input, Want string }
	}
	data, err := os.ReadFile("testdata/upstream-main/validation-strings.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Upstream != "c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8" || len(fixture.Literal) != 2 || len(fixture.Dependency) != 12 || len(fixture.Derived) != 27 || len(fixture.NativeBigInt) != 2 || len(fixture.NativeFloat) != 4 {
		t.Fatal("unexpected pinned string fixture shape")
	}
	for _, group := range []struct {
		name string
		rows []row
	}{{"literal", fixture.Literal}, {"dependency", fixture.Dependency}, {"source-derived", fixture.Derived}} {
		t.Run(group.name, func(t *testing.T) {
			for _, tc := range group.rows {
				t.Run(tc.Name, func(t *testing.T) {
					t.Parallel()
					got, args, err := mainValidateJSON(t, string(tc.Schema), string(tc.Input))
					mainAssertValidationJSON(t, args, string(tc.Input))
					if !strings.HasPrefix(tc.Want, "ok ") {
						t.Fatal("unexpected success-only assertion")
					}
					if err != nil {
						t.Fatal(err)
					}
					mainAssertValidationJSON(t, got, strings.TrimPrefix(tc.Want, "ok "))
				})
			}
		})
	}
	for _, tc := range fixture.NativeBigInt {
		t.Run(tc.Name+"-int64-adaptation", func(t *testing.T) {
			value, err := strconv.ParseInt(tc.Input, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			mainAssertStringConversion(t, value, tc.Want)
		})
	}
	for _, tc := range fixture.NativeFloat {
		t.Run("source-derived/"+tc.Name, func(t *testing.T) {
			values := map[string]float64{"negative-zero": math.Copysign(0, -1), "NaN": math.NaN(), "positive-infinity": math.Inf(1), "negative-infinity": math.Inf(-1)}
			value, ok := values[tc.Name]
			if !ok {
				t.Fatal("unknown float witness")
			}
			mainAssertStringConversion(t, value, tc.Want)
		})
	}
}

func mainAssertStringConversion(t *testing.T, value interface{}, want string) {
	t.Helper()
	tool := goai.Tool{Name: "string", Parameters: json.RawMessage(`{"type":"object","properties":{"h":{"type":"string"}},"required":["h"]}`)}
	args := map[string]interface{}{"h": value}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if got["h"] != want {
		t.Fatalf("got %#v, want %q", got["h"], want)
	}
	if number, ok := value.(float64); ok {
		if current, ok := args["h"].(float64); !ok || math.Float64bits(current) != math.Float64bits(number) {
			t.Fatal("input float changed")
		}
	} else if args["h"] != value {
		t.Fatal("input changed")
	}
}

// Exact native numerical extensions are never routed through float64. JSON
// decimal spelling is canonicalised without changing the represented value.
func TestUpstreamMainValidationStringsNativePrecision(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value interface{}
		want  string
	}{
		{"int", int(12), "12"},
		{"int64-max", int64(math.MaxInt64), "9223372036854775807"},
		{"int64-min", int64(math.MinInt64), "-9223372036854775808"},
		{"decimal-wide", json.Number("9007199254740993"), "9007199254740993"},
		{"decimal-fraction", json.Number("1.00000000000000000000001"), "1.00000000000000000000001"},
		{"decimal-exponent", json.Number("1.2300e3"), "1230"},
		{"decimal-negative-zero", json.Number("-0.00e99"), "0"},
		{"decimal-small", json.Number("0.0000001"), "1e-7"},
		{"decimal-limit-fixed", json.Number("1e20"), "100000000000000000000"},
		{"decimal-limit-exponent", json.Number("1e21"), "1e+21"},
		{"decimal-large-exponent", json.Number("1e9999"), "1e+9999"},
	} {
		t.Run(tc.name, func(t *testing.T) { mainAssertStringConversion(t, tc.value, tc.want) })
	}
}
