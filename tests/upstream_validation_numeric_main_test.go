package goai_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func mainNumericOutcome(t *testing.T, schema, input string) string {
	t.Helper()
	got, args, err := mainValidateJSON(t, schema, input)
	mainAssertValidationJSON(t, args, input)
	if err != nil {
		lines := strings.Split(err.Error(), "\n")
		if len(lines) > 1 && strings.HasPrefix(lines[0], "Validation failed") {
			return "REJECT " + strings.TrimSpace(lines[1])
		}
		return "THROW " + lines[0]
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	return "ok " + string(encoded)
}

func TestUpstreamMainValidationNumericBounds(t *testing.T) {
	cases := []struct{ name, schema, input, want string }{
		// Exact schemas/inputs/outcomes from the source probe matrix: numeric
		// exclusiveMinimum (347,570-571) and ignored draft4 boolean (217,524-525).
		{"exclusive-number", `{"type":"object","properties":{"n":{"type":"number","exclusiveMinimum":0}}}`, `{"n":0}`, `REJECT - n: expected > 0`},
		{"exclusive-number-string", `{"type":"object","properties":{"n":{"type":"number","exclusiveMinimum":0}}}`, `{"n":"0"}`, `REJECT - n: expected > 0`},
		{"draft4-bool-zero", `{"$schema":"http://json-schema.org/draft-04/schema#","type":"object","properties":{"n":{"type":"number","minimum":0,"exclusiveMinimum":true}}}`, `{"n":0}`, `ok {"n":0}`},
		{"draft4-bool-one", `{"$schema":"http://json-schema.org/draft-04/schema#","type":"object","properties":{"n":{"type":"number","minimum":0,"exclusiveMinimum":true}}}`, `{"n":1}`, `ok {"n":1}`},
		// First assertion of the source cache probe (727-732), plus fresh-schema
		// controls: ignoring minimum must not masquerade as cache fidelity.
		{"cache-initial-minimum", `{"type":"object","properties":{"n":{"type":"number","minimum":0}},"required":["n"]}`, `{"n":"4"}`, `ok {"n":4}`},
		{"fresh-minimum", `{"type":"object","properties":{"n":{"type":"number","minimum":10}},"required":["n"]}`, `{"n":4}`, `REJECT - n: expected >= 10`},
		{"fresh-minimum-string", `{"type":"object","properties":{"n":{"type":"number","minimum":10}},"required":["n"]}`, `{"n":"4"}`, `REJECT - n: expected >= 10`},
		{"minimum-equal", `{"type":"object","properties":{"n":{"type":"number","minimum":10}}}`, `{"n":10}`, `ok {"n":10}`},
		{"maximum", `{"type":"object","properties":{"n":{"type":"number","maximum":5}}}`, `{"n":6}`, `REJECT - n: expected <= 5`},
		{"maximum-equal", `{"type":"object","properties":{"n":{"type":"number","maximum":5}}}`, `{"n":5}`, `ok {"n":5}`},
		{"exclusive-maximum", `{"type":"object","properties":{"n":{"type":"number","exclusiveMaximum":5}}}`, `{"n":"5"}`, `REJECT - n: expected < 5`},
		{"untyped-minimum", `{"type":"object","properties":{"n":{"minimum":10}}}`, `{"n":4}`, `REJECT - n: expected >= 10`},
		{"fractional-equality", `{"type":"object","properties":{"n":{"type":"number","minimum":0.1,"maximum":0.1}}}`, `{"n":0.1}`, `ok {"n":0.1}`},
		{"oneOf-bounds", `{"type":"object","oneOf":[{"properties":{"n":{"type":"number","minimum":10}},"required":["n"]},{"properties":{"n":{"type":"number","maximum":5}},"required":["n"]}]}`, `{"n":"4"}`, `ok {"n":4}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mainNumericOutcome(t, tc.schema, tc.input); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// Native numerical extensions: JS cannot represent all these integer values.
// Checking a bound must neither round them into acceptance nor rewrite them.
func TestUpstreamMainValidationNumericNativePrecision(t *testing.T) {
	cases := []struct {
		name, keyword string
		value         interface{}
		bound         float64
		accept        bool
	}{
		{"int64-above-maximum", "maximum", int64(math.MaxInt64), 9007199254740992, false},
		{"number-above-maximum", "maximum", json.Number("9007199254740993"), 9007199254740992, false},
		{"int64-above-exclusive", "exclusiveMinimum", int64(math.MaxInt64), 9007199254740992, true},
		{"number-above-exclusive", "exclusiveMinimum", json.Number("9007199254740993"), 9007199254740992, true},
		{"large-canonical-integer", "maximum", json.Number("9223372036854775809"), 9007199254740992, false},
		{"large-rounding-maximum", "maximum", json.Number("9223372036854775809"), 9223372036854775808, false},
		{"large-rounding-exclusive", "exclusiveMinimum", json.Number("9223372036854775809"), 9223372036854775808, true},
		{"negative-rounding-minimum", "minimum", json.Number("-9223372036854775809"), -9223372036854775808, false},
		{"int64-rounded-minimum", "minimum", int64(math.MaxInt64), 9223372036854775808, false},
		{"int64-rounded-maximum", "maximum", int64(math.MaxInt64), 9223372036854775808, true},
		{"int64-near-negative-bound", "exclusiveMinimum", int64(math.MinInt64 + 1), -9223372036854775808, true},
		{"int64-min-exclusive", "exclusiveMinimum", int64(math.MinInt64), -9223372036854775808, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema, err := json.Marshal(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"n": map[string]interface{}{"type": "integer", tc.keyword: tc.bound}}})
			if err != nil {
				t.Fatal(err)
			}
			tool := goai.Tool{Name: "precision", Parameters: schema}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"n": tc.value}})
			if !tc.accept {
				if err == nil {
					t.Fatalf("out-of-bound value accepted: %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got["n"] != tc.value {
				t.Fatalf("bound check rounded %T %v to %T %v", tc.value, tc.value, got["n"], got["n"])
			}
		})
	}
}

func TestUpstreamMainValidationNumericBoundNonfinite(t *testing.T) {
	tool := goai.Tool{Name: "finite", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"number","minimum":0}}}`)}
	for _, tc := range []struct {
		name  string
		value interface{}
	}{
		{"NaN", math.NaN()}, {"positive-infinity", math.Inf(1)}, {"negative-infinity", math.Inf(-1)}, {"number-overflow", json.Number("1e9999")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"n": tc.value}}); err == nil {
				t.Fatalf("nonfinite numeric bound value accepted: %#v", got)
			}
		})
	}
	value := json.Number("0.1")
	tool.Parameters = json.RawMessage(`{"type":"object","properties":{"n":{"type":"number","minimum":0.1,"maximum":0.1}}}`)
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"n": value}})
	if err != nil {
		t.Fatal(err)
	}
	if got["n"] != value {
		t.Fatalf("decimal bound rewrote value: %#v", got)
	}
}
