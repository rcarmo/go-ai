package goai_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Native number representations that already satisfy a type must remain exact;
// routing them through float64 would lose wide int64/json.Number values.
func TestUpstreamMainValidationAlreadyTypedNumbers(t *testing.T) {
	for _, typ := range []string{"integer", "number"} {
		for _, input := range []interface{}{int64(math.MaxInt64), json.Number("9007199254740993"), float64(3)} {
			tool := goai.Tool{Name: "numeric", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"type":"` + typ + `"}}}`)}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"n": input}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got["n"], input) {
				t.Errorf("%s converted already-valid %T(%v) to %T(%v)", typ, input, input, got["n"], got["n"])
			}
		}
	}
}

// A branch-only check would return the first ambiguous oneOf candidate and
// reject, instead of trying the later candidate accepted by the whole node.
func TestUpstreamMainValidationWholeNodeCandidate(t *testing.T) {
	for _, keyword := range []string{"anyOf", "oneOf"} {
		t.Run(keyword, func(t *testing.T) {
			schema := `{"type":"object","` + keyword + `":[{"type":"object","properties":{"a":{"type":"integer"},"c":{"type":"integer"}},"required":["a","c"]},{"type":"object","properties":{"b":{"type":"integer"},"c":{"type":"integer"}},"required":["b","c"]}],"properties":{"b":{"type":"integer"}},"required":["b"]}`
			got, args, err := mainValidateJSON(t, schema, `{"a":"1","b":"2","c":"3"}`)
			if err != nil {
				t.Fatal(err)
			}
			want := `{"a":1,"b":2,"c":3}`
			if keyword == "oneOf" {
				want = `{"a":"1","b":2,"c":3}`
			}
			mainAssertValidationJSON(t, got, want)
			mainAssertValidationJSON(t, args, `{"a":"1","b":"2","c":"3"}`)
		})
	}
}

// Source-derived controls for coerce.ts convertUnion: a candidate is judged
// against the whole node, not just the branch that attempted its conversion.
func TestUpstreamMainValidationUnionCandidates(t *testing.T) {
	for _, keyword := range []string{"anyOf", "oneOf"} {
		for _, tc := range []struct{ name, branches, input, want string }{
			{
				"non-object-branch-does-not-walk-object-members",
				`[{"type":"boolean","properties":{"a":{"type":"array","items":{"type":"integer"}}}},{"type":"object","properties":{"c":{"type":"integer"}},"required":["a","c"]}]`,
				`{"a":"3","c":"2"}`, `{"a":"3","c":2}`,
			},
			{
				"partly-invalid-conversion-can-match-another-branch",
				`[{"type":"object","properties":{"a":{"type":"array","items":{"type":"integer"}},"x":{"type":"integer"}},"required":["a","x"]},{"type":"object","properties":{"c":{"type":"integer"}},"required":["c"]}]`,
				`{"a":"invalid","x":"4","c":"2"}`, `{"a":["invalid"],"x":4,"c":2}`,
			},
		} {
			t.Run(keyword+"/"+tc.name, func(t *testing.T) {
				schema := `{"type":"object","` + keyword + `":` + tc.branches + `,"properties":{"c":{"type":"integer"}},"required":["c"]}`
				got, args, err := mainValidateJSON(t, schema, tc.input)
				if err != nil {
					t.Fatal(err)
				}
				mainAssertValidationJSON(t, args, tc.input)
				mainAssertValidationJSON(t, got, tc.want)
			})
		}
	}
}
