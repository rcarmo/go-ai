package goai_test

import (
	"encoding/json"
	"math"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type mainStringTrap struct{}

func (mainStringTrap) String() string { panic("object stringification must not run") }

func TestUpstreamMainValidationStringsRejectionFences(t *testing.T) {
	tool := goai.Tool{Name: "string", Parameters: json.RawMessage(`{"type":"object","properties":{"h":{"type":"string"}},"required":["h"]}`)}
	for _, tc := range []struct {
		name  string
		value interface{}
	}{
		{"object", map[string]interface{}{}}, {"array", []interface{}{}}, {"stringer", mainStringTrap{}},
		{"empty-decimal", json.Number("")}, {"invalid-decimal", json.Number("12x")},
		{"nonfinite-decimal", json.Number("NaN")}, {"leading-zero-decimal", json.Number("01")},
		{"exponent-overflow", json.Number("1e9223372036854775807")},
		{"exponent-underflow", json.Number("0.01e-9223372036854775808")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"h": tc.value}}); err == nil {
				t.Fatalf("unconvertible input accepted: %#v", got)
			}
		})
	}
}

func TestUpstreamMainValidationStringsOwnership(t *testing.T) {
	tool := goai.Tool{Name: "string", Parameters: json.RawMessage(`{"type":"object","properties":{"h":{"type":"array","items":{"type":"string"}}},"required":["h"]}`)}
	array := []interface{}{float64(12), nil, true}
	args := map[string]interface{}{"h": array}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	mainAssertValidationJSON(t, got, `{"h":["12","null","true"]}`)
	got["h"].([]interface{})[0] = "changed"
	mainAssertValidationJSON(t, args, `{"h":[12,null,true]}`)
}

func TestUpstreamMainValidationStringsMatchingNumber(t *testing.T) {
	for _, schema := range []string{
		`{"type":"object","properties":{"h":{"anyOf":[{"type":"string"},{"type":"number"}]}},"required":["h"]}`,
		`{"type":"object","properties":{"h":{"type":["string","number"]}},"required":["h"]}`,
	} {
		tool := goai.Tool{Name: "string", Parameters: json.RawMessage(schema)}
		for _, value := range []interface{}{int64(math.MaxInt64), json.Number("9007199254740993")} {
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"h": value}})
			if err != nil {
				t.Fatal(err)
			}
			if got["h"] != value {
				t.Fatalf("matching native number rewritten: %T %v -> %T %v", value, value, got["h"], got["h"])
			}
		}
	}
}
