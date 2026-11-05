package goai_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Exact values from pinned draft2020-12 patternProperties.json, wrapped as a
// tool argument. Coercion is disabled by feeding native integer values.
func TestMainValidationPatternPropertiesOverlapping(t *testing.T) {
	schema := `{"type":"object","properties":{"v":{"type":"object","patternProperties":{"a*":{"type":"integer"},"aaa*":{"maximum":20}}}},"required":["v"]}`
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"v":{"a":21}}`, true},
		{`{"v":{"aaaa":18}}`, true},
		{`{"v":{"aaaa":31}}`, false},
	} {
		got := mainNumericOutcome(t, schema, tc.input)
		if strings.HasPrefix(got, "ok ") != tc.valid {
			t.Errorf("input=%s got=%q", tc.input, got)
		}
	}
}

// Source-derived from sharedRegex, with a matching nested optional null.
func TestMainValidationPatternPropertiesNormalizeAndOwn(t *testing.T) {
	tool := goai.Tool{Name: "pattern", Parameters: json.RawMessage(`{"type":"object","properties":{"v":{"type":"object","patternProperties":{"(?<!x)y":{"type":"object","properties":{"n":{"type":"integer"}}}},"additionalProperties":false}}}`)}
	args := map[string]interface{}{"v": map[string]interface{}{"y": map[string]interface{}{"n": nil}}}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	want := map[string]interface{}{"v": map[string]interface{}{"y": map[string]interface{}{}}}
	if err != nil || !reflect.DeepEqual(got, want) || !reflect.DeepEqual(args, map[string]interface{}{"v": map[string]interface{}{"y": map[string]interface{}{"n": nil}}}) {
		t.Fatalf("got=%#v input=%#v err=%v", got, args, err)
	}
}
