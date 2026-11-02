package goai_test

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source-derived local-reference controls, supplementing the exact depth-40
// upstream test. They exercise real resolution/coercion rather than merely
// rejecting that input early because its outer required properties are absent.
func TestUpstreamMainValidationReferences(t *testing.T) {
	cases := []struct{ name, schema, input, want string }{
		{"scalar", `{"type":"object","properties":{"n":{"$ref":"#/$defs/N"}},"$defs":{"N":{"type":"integer"}}}`, `{"n":"7"}`, `{"n":7}`},
		{"productive", `{"type":"object","properties":{"s":{"$ref":"#/$defs/S"}},"$defs":{"S":{"anyOf":[{"type":"null"},{"type":"object","properties":{"n":{"type":"integer"},"next":{"$ref":"#/$defs/S"}},"required":["n"]}]}}}`, `{"s":{"n":"2","next":{"n":"3","next":null}}}`, `{"s":{"n":2,"next":{"n":3,"next":null}}}`},
		{"escaped-pointer", `{"type":"object","properties":{"n":{"$ref":"#/$defs/a~1b~0c"}},"$defs":{"a/b~c":{"type":"integer"}}}`, `{"n":"4"}`, `{"n":4}`},
		{"root-pointer", `{"type":"object","properties":{"n":{"type":"integer"},"child":{"$ref":"#"}}}`, `{"n":"2","child":{"n":"3"}}`, `{"n":2,"child":{"n":3}}`},
		{"missing-optional-unresolved", `{"type":"object","properties":{"n":{"$ref":"#/$defs/missing"}}}`, `{}`, `{}`},
		{"present-unresolved", `{"type":"object","properties":{"n":{"$ref":"#/$defs/missing"}}}`, `{"n":1}`, ""},
		{"false-target", `{"type":"object","properties":{"n":{"$ref":"#/$defs/D"}},"$defs":{"D":false}}`, `{"n":1}`, ""},
		{"true-target", `{"type":"object","properties":{"n":{"$ref":"#/$defs/D"}},"$defs":{"D":true}}`, `{"n":{"v":2}}`, `{"n":{"v":2}}`},
		{"additional-conversion", `{"type":"object","additionalProperties":{"$ref":"#/$defs/N"},"$defs":{"N":{"type":"integer"}}}`, `{"x":"3"}`, `{"x":3}`},
		{"pattern-conversion", `{"type":"object","patternProperties":{"^x$":{"$ref":"#/$defs/N"}},"additionalProperties":false,"$defs":{"N":{"type":"integer"}}}`, `{"x":"3"}`, `{"x":3}`},
		{"closed-object", `{"type":"object","additionalProperties":false}`, `{"x":3}`, ""},
		{"ref-sibling-reject", `{"type":"object","properties":{"n":{"$ref":"#/$defs/N","enum":["b"]}},"$defs":{"N":{"type":"string"}}}`, `{"n":"a"}`, ""},
		{"array-pointer", `{"type":"object","properties":{"n":{"$ref":"#/allOf/0/properties/n"}},"allOf":[{"type":"object","properties":{"n":{"type":"integer"}}}]}`, `{"n":"4"}`, `{"n":4}`},
		{"uri-pointer", `{"type":"object","properties":{"n":{"$ref":"#/$defs/%4E"}},"$defs":{"N":{"type":"integer"}}}`, `{"n":"4"}`, `{"n":4}`},
		{"literal-tilde-pointer", `{"type":"object","properties":{"n":{"$ref":"#/$defs/N~2"}},"$defs":{"N~2":{"type":"integer"}}}`, `{"n":"4"}`, `{"n":4}`},
		{"scalar-untyped-array", `{"type":"object","properties":{"n":{"type":"number"},"b":{"type":"boolean"},"a":{"type":"array"},"o":{"type":"object"},"s":{"type":"string","enum":["x","y"]}},"required":["n"]}`, `{"n":1,"a":"not array"}`, `{"n":1,"a":["not array"]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, args, err := mainValidateJSON(t, tc.schema, tc.input)
			mainAssertValidationJSON(t, args, tc.input)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("invalid reference value accepted: %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			mainAssertValidationJSON(t, got, tc.want)
		})
	}
}

func TestUpstreamMainValidationReferenceOwnershipAndCalls(t *testing.T) {
	tool := goai.Tool{Name: "refs", Parameters: json.RawMessage(`{"type":"object","properties":{"s":{"$ref":"#/$defs/N"}},"$defs":{"N":{"type":"object","properties":{"n":{"type":"integer"}}}}}`)}
	nested := map[string]interface{}{"n": "7"}
	args := map[string]interface{}{"s": nested}
	call := goai.ToolCall{Name: tool.Name, Arguments: args}
	first, err := goai.ValidateToolArguments(&tool, call)
	if err != nil {
		t.Fatal(err)
	}
	mainAssertValidationJSON(t, first, `{"s":{"n":7}}`)
	mainAssertValidationJSON(t, args, `{"s":{"n":"7"}}`)
	first["s"].(map[string]interface{})["n"] = 9
	nested["n"] = "8"
	second, err := goai.ValidateToolArguments(&tool, call)
	if err != nil {
		t.Fatal(err)
	}
	mainAssertValidationJSON(t, second, `{"s":{"n":8}}`)
	mainAssertValidationJSON(t, first, `{"s":{"n":9}}`)
	mainAssertValidationJSON(t, args, `{"s":{"n":"8"}}`)
}

func TestUpstreamMainValidationReferenceWideNumbers(t *testing.T) {
	tool := goai.Tool{Name: "refs", Parameters: json.RawMessage(`{"type":"object","properties":{"n":{"$ref":"#/$defs/N"}},"$defs":{"N":{"type":"integer"}}}`)}
	for _, number := range []interface{}{int64(math.MaxInt64), json.Number("9007199254740993")} {
		got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]interface{}{"n": number}})
		if err != nil {
			t.Fatal(err)
		}
		if got["n"] != number {
			t.Fatalf("reference conversion rounded %T %v to %T %v", number, number, got["n"], got["n"])
		}
	}
}

func TestUpstreamMainValidationReferenceNeverFetches(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"type":"integer"}`))
	}))
	defer server.Close()
	schema, err := json.Marshal(map[string]interface{}{"type": "object", "properties": map[string]interface{}{"n": map[string]interface{}{"$ref": server.URL + "/schema"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = mainValidateJSON(t, string(schema), `{"n":"7"}`)
	if err == nil {
		t.Fatal("unknown external reference accepted")
	}
	if requests.Load() != 0 {
		t.Fatal("validation fetched an external resource")
	}
}
