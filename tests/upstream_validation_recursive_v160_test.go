package goai_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const v160RecursiveTreeSchema = `{
	"type":"object",
	"properties":{"t":{"$ref":"#/$defs/Tree"}},
	"$defs":{"Tree":{"anyOf":[{"type":"string"},{"type":"array","items":{"$ref":"#/$defs/Tree"}}]}}
}`

// packages/schema/test/coerce.test.ts:359–388, excluding the separate raw
// coerce assertion at L374. ValidateToolArguments is the public JSON parse
// seam: success returns the converted value, failure returns a tool error.
func TestSourceV160SchemaRecursiveCoercion(t *testing.T) {
	const jsonSchema = `{
		"type":"object",
		"properties":{"data":{"$ref":"#/$defs/Json"}},
		"required":["data"],
		"$defs":{"Json":{"anyOf":[
			{"type":"string"},{"type":"number"},{"type":"boolean"},
			{"type":"array","items":{"$ref":"#/$defs/Json"}},
			{"type":"object","additionalProperties":{"$ref":"#/$defs/Json"}}
		]}}
	}`
	const scalarOrArray = `{
		"type":"object",
		"properties":{"value":{"anyOf":[{"type":"number"},{"type":"array","items":{"$ref":"#/properties/value"}}]}},
		"required":["value"]
	}`
	for _, tc := range []struct{ name, schema, input, want string }{
		{"L361-keeps-matching-recursive-array", v160RecursiveTreeSchema, `{"t":["a",["b"]]}`, `{"t":["a",["b"]]}`},
		{"L362-converts-to-first-accepting-branch", v160RecursiveTreeSchema, `{"t":5}`, `{"t":"5"}`},
		{"L363-converts-recursive-array-item", v160RecursiveTreeSchema, `{"t":[5]}`, `{"t":["5"]}`},
		{"L364-converts-additional-property-values", jsonSchema, `{"data":{"a":[1,null]}}`, `{"data":{"a":[1,"null"]}}`},
		{"L373-rejects-unconvertible-tree", v160RecursiveTreeSchema, `{"t":{}}`, ""},
		{"L386-converts-scalar", scalarOrArray, `{"value":"42"}`, `{"value":42}`},
		{"L387-converts-nested-scalars", scalarOrArray, `{"value":["1",["2"]]}`, `{"value":[1,[2]]}`},
		{"L388-rejects-unconvertible-scalar", scalarOrArray, `{"value":{}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := goai.Tool{Name: "recursive", Parameters: json.RawMessage(tc.schema)}
			checkV160RecursiveParseValue(t, &tool, tc.input, tc.want)
		})
	}
}

// coerce.test.ts:405–410. The same Go tool definition is reused on all calls;
// each public Go call creates its own validator, unlike upstream compile reuse.
func TestSourceV160SchemaRecursiveCoercionRepeated(t *testing.T) {
	tool := goai.Tool{Name: "recursive", Parameters: json.RawMessage(
		`{"anyOf":[{"type":"number"},{"type":"array","items":{"$ref":"#"}}]}`,
	)}
	for i := range 20 {
		t.Run(fmt.Sprintf("iteration-%02d", i), func(t *testing.T) {
			checkV160RecursiveParseValue(t, &tool, `{}`, "")
			checkV160RecursiveParseValue(t, &tool, `"42"`, `42`)
			const input = `[1,"2",["3"]]`
			original := checkV160RecursiveParseValue(t, &tool, input, `[1,2,[3]]`)
			mainAssertValidationJSON(t, original, input)
		})
	}
}

func checkV160RecursiveParseValue(t *testing.T, tool *goai.Tool, input, want string) any {
	t.Helper()
	var original any
	if err := json.Unmarshal([]byte(input), &original); err != nil {
		t.Fatal(err)
	}
	got, err := goai.ValidateToolArguments(tool, goai.ToolCall{Name: tool.Name, Arguments: original})
	if want == "" {
		if err == nil || !strings.HasPrefix(err.Error(), "Validation failed for tool ") {
			t.Fatalf("parse(%s) = %#v, %v; want schema rejection", input, got, err)
		}
	} else {
		if err != nil {
			t.Fatalf("parse(%s): %v", input, err)
		}
		mainAssertValidationJSON(t, got, want)
	}
	return original
}
