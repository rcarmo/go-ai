package goai_test

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func mainBranchSiblingSchema(keyword string) string {
	return `{"type":"object","` + keyword + `":[{"type":"object","properties":{"a":{"type":"array","items":{"type":"integer"}}},"required":["a"]},{"type":"object","properties":{"b":{"type":"boolean"}},"required":["b"]}],"properties":{"c":{"type":"integer"}},"required":["c"]}`
}

func mainValidateJSON(t *testing.T, schema, input string) (map[string]interface{}, map[string]interface{}, error) {
	t.Helper()
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(input), &args); err != nil {
		t.Fatal(err)
	}
	tool := goai.Tool{Name: "union", Parameters: json.RawMessage(schema)}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: args})
	return got, args, err
}

func mainAssertValidationJSON(t *testing.T, got interface{}, expected string) {
	t.Helper()
	// Go represents coerced integer primitives as int and decoded JSON numbers
	// as float64. Compare JSON values, preserving presence/null/array structure.
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var actual, want interface{}
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("got %s, want %s", data, expected)
	}
}

// Exact schema, input and result from c9f4c7b9 packages/ai/test/
// validation-probes.test.ts:752-765. No custom conversion helper is used.
func TestUpstreamMainValidationBranchSibling(t *testing.T) {
	for _, keyword := range []string{"anyOf", "oneOf"} {
		t.Run(keyword, func(t *testing.T) {
			got, _, err := mainValidateJSON(t, mainBranchSiblingSchema(keyword), `{"a":"3","c":"2"}`)
			if err != nil {
				t.Fatal(err)
			}
			mainAssertValidationJSON(t, got, `{"a":[3],"c":2}`)
		})
	}
}

// Native source-derived supplements to the two upstream assertions: branch
// selection, non-coercing membership checks, sibling requirements and ownership.
func TestUpstreamMainValidationBranchSiblingControls(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"second-branch", `{"b":"true","c":"2"}`, `{"b":true,"c":2}`},
		{"already-matches-branch", `{"a":[3],"c":"2"}`, `{"a":[3],"c":2}`},
		{"later-branch-after-failed-array", `{"a":"invalid","b":"true","c":"2"}`, `{"a":"invalid","b":true,"c":2}`},
		{"first-valid-candidate", `{"a":"3","b":"true","c":"2"}`, `{"a":[3],"b":"true","c":2}`},
		{"invalid-branch", `{"a":"invalid","c":"2"}`, ""},
		{"missing-sibling", `{"a":"3"}`, ""},
		{"invalid-sibling", `{"a":"3","c":"invalid"}`, ""},
		{"missing-both-branches", `{"c":"2"}`, ""},
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		for _, tc := range cases {
			t.Run(keyword+"/"+tc.name, func(t *testing.T) {
				got, args, err := mainValidateJSON(t, mainBranchSiblingSchema(keyword), tc.input)
				mainAssertValidationJSON(t, args, tc.input)
				if tc.want == "" {
					if err == nil {
						t.Fatalf("invalid candidate accepted: %#v", got)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				mainAssertValidationJSON(t, got, tc.want)
			})
		}
		t.Run(keyword+"/nested", func(t *testing.T) {
			schema := `{"type":"object","properties":{"payload":` + mainBranchSiblingSchema(keyword) + `}}`
			got, args, err := mainValidateJSON(t, schema, `{"payload":{"a":"3","c":"2"}}`)
			if err != nil {
				t.Fatal(err)
			}
			mainAssertValidationJSON(t, got, `{"payload":{"a":[3],"c":2}}`)
			mainAssertValidationJSON(t, args, `{"payload":{"a":"3","c":"2"}}`)
		})
		t.Run(keyword+"/ambiguous-branches", func(t *testing.T) {
			schema := `{"type":"object","` + keyword + `":[{"type":"object","properties":{"a":{"type":"integer"}},"required":["a"]},{"type":"object"}],"properties":{"c":{"type":"integer"}},"required":["c"]}`
			got, _, err := mainValidateJSON(t, schema, `{"a":3,"c":"2"}`)
			if keyword == "oneOf" {
				if err == nil {
					t.Fatalf("ambiguous oneOf accepted: %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			mainAssertValidationJSON(t, got, `{"a":3,"c":2}`)
		})
	}
}
