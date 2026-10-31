package google

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func assertGoogleSchemaMain(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	decode := func(raw string) any {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	actual, expected := decode(string(got)), decode(want)
	if !googleSchemaEqualMain(actual, expected) {
		t.Fatalf("schema mismatch: got %s want %s", got, want)
	}
}
func TestGoogleConstNameMapsMain(t *testing.T) {
	for _, keyword := range []string{"properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies"} {
		t.Run(keyword, func(t *testing.T) {
			raw := fmt.Sprintf(`{"%s":{"__proto__":{"const":"x"},"const":{"const":"y"},"properties":{"const":"z"}}}`, keyword)
			want := fmt.Sprintf(`{"%s":{"__proto__":{"enum":["x"]},"const":{"enum":["y"]},"properties":{"enum":["z"]}}}`, keyword)
			assertGoogleSchemaMain(t, rewriteGoogleConstSchema(json.RawMessage(raw)), want)
		})
	}
}
func TestGoogleConstSchemaKeywordsMain(t *testing.T) {
	for _, keyword := range []string{"additionalProperties", "unevaluatedProperties", "propertyNames", "items", "prefixItems", "additionalItems", "unevaluatedItems", "contains", "allOf", "anyOf", "oneOf", "not", "if", "then", "else", "contentSchema"} {
		t.Run(keyword, func(t *testing.T) {
			for _, array := range []bool{false, true} {
				child, want := `{"const":2}`, `{"enum":[2]}`
				if array {
					child, want = "["+child+"]", "["+want+"]"
				}
				raw := fmt.Sprintf(`{"%s":%s}`, keyword, child)
				assertGoogleSchemaMain(t, rewriteGoogleConstSchema(json.RawMessage(raw)), fmt.Sprintf(`{"%s":%s}`, keyword, want))
			}
		})
	}
}
func TestGoogleConstLiteralsAndExistingEnumsMain(t *testing.T) {
	for _, keyword := range []string{"default", "examples", "enum", "const", "x-custom"} {
		t.Run(keyword, func(t *testing.T) {
			literal := `{"const":"literal-data","properties":{"nested":{"const":1}}}`
			if keyword == "examples" || keyword == "enum" {
				literal = "[" + literal + "]"
			}
			raw := fmt.Sprintf(`{"properties":{"value":{"%s":%s}}}`, keyword, literal)
			assertGoogleSchemaMain(t, rewriteGoogleConstSchema(json.RawMessage(raw)), raw)
		})
	}
	for _, raw := range []string{`{"const":true}`, `{"const":false}`, `{"const":null}`, `{"const":[1]}`, `{"const":"x","enum":null}`, `{"const":1,"enum":[2]}`, `{"dependencies":{"a":["b"]},"unevaluatedProperties":false}`} {
		assertGoogleSchemaMain(t, rewriteGoogleConstSchema(json.RawMessage(raw)), raw)
	}
}
func TestGoogleConstNumbersAndOwnershipMain(t *testing.T) {
	raw := json.RawMessage(`{"$schema":"schema","properties":{"large":{"const":9007199254740993},"decimal":{"const":1.234567890123456789},"exponent":{"const":1e+30},"negativeZero":{"const":-0},"untouched":{"default":9007199254740993}}}`)
	original := append(json.RawMessage(nil), raw...)
	tool := goai.Tool{Name: "tool", Parameters: raw}
	got := convertTools([]goai.Tool{tool})[0].FunctionDeclarations[0].ParametersJsonSchema
	assertGoogleSchemaMain(t, got, `{"$schema":"schema","properties":{"large":{"enum":[9007199254740993]},"decimal":{"enum":[1.234567890123456789]},"exponent":{"enum":[1e+30]},"negativeZero":{"enum":[-0]},"untouched":{"default":9007199254740993}}}`)
	if string(raw) != string(original) {
		t.Fatal("caller schema mutated")
	}
	got[0] = ' '
	if string(raw) != string(original) {
		t.Fatal("result aliases caller schema")
	}
}
func TestGoogleLegacySanitizationMain(t *testing.T) {
	raw := json.RawMessage(`{"$schema":"s","$id":"i","$anchor":"a","$dynamicAnchor":"d","$vocabulary":{},"$comment":"c","$defs":{},"definitions":{},"properties":{"value":{"$anchor":"nested","const":9007199254740993,"$ref":"#/def"}},"allOf":[{"$schema":"preserved-array","const":"x"}]}`)
	got := convertTools([]goai.Tool{{Name: "tool", Parameters: raw}}, true)[0].FunctionDeclarations[0]
	if len(got.ParametersJsonSchema) != 0 {
		t.Fatal("legacy dialect changed")
	}
	assertGoogleSchemaMain(t, got.Parameters, `{"properties":{"value":{"const":9007199254740993,"$ref":"#/def"}},"allOf":[{"$schema":"preserved-array","const":"x"}]}`)
}
func TestGoogleConstRequestAPIsMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		t.Run(string(api), func(t *testing.T) {
			model := &goai.Model{ID: "gemini-2.5-pro", Api: api, Provider: goai.ProviderGoogle}
			request, err := buildRequest(model, &goai.Context{Tools: []goai.Tool{{Name: "tool", Parameters: json.RawMessage(`{"properties":{"origin":{"const":"source"}}}`)}}}, &goai.StreamOptions{})
			if err != nil {
				t.Fatal(err)
			}
			assertGoogleSchemaMain(t, request.Tools[0].FunctionDeclarations[0].ParametersJsonSchema, `{"properties":{"origin":{"enum":["source"]}}}`)
		})
	}
}
func TestGoogleSchemaMalformedCopyMain(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`{"const":`), json.RawMessage(`{} {}`)} {
		for _, convert := range []func(json.RawMessage) json.RawMessage{rewriteGoogleConstSchema, stripGoogleSchemaMeta} {
			got := convert(raw)
			if string(got) != string(raw) {
				t.Fatal("malformed schema changed")
			}
			got[0] = ' '
			if raw[0] == ' ' {
				t.Fatal("malformed result aliases input")
			}
		}
	}
}

// Compare JSON numeric values independently, not their generated spellings.
func googleSchemaEqualMain(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		ar, ok := new(big.Rat).SetString(string(av))
		if !ok {
			return false
		}
		br, ok := new(big.Rat).SetString(string(bv))
		return ok && ar.Cmp(br) == 0
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for key, value := range av {
			other, exists := bv[key]
			if !exists || !googleSchemaEqualMain(value, other) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i, value := range av {
			if !googleSchemaEqualMain(value, bv[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a, b)
	}
}
