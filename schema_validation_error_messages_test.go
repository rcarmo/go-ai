package goai_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:384–400, through standalone check and explanation calls.
func TestSourceV160SchemaEnumMessages(t *testing.T) {
	values := make([]string, 5000)
	for i := range values {
		values[i] = "v" + strconv.Itoa(i)
	}
	enum := encodeV160Schema(t, values)
	schema := json.RawMessage(`{"type":"object","properties":{"e":{"type":"string","enum":` + string(enum) + `}}}`)
	t.Run("L387-final-member", func(t *testing.T) {
		assertV160SchemaCheck(t, string(schema), `{"e":"v4999"}`, nil, "", false, true)
	})
	t.Run("L389-capped-enum-message", func(t *testing.T) {
		listed := strings.TrimSuffix(string(encodeV160Schema(t, values[:20])), "]")
		want := []goai.JSONSchemaDiagnostic{{Path: "/e", Keyword: "enum", Message: "expected one of " + listed + ", ... and 4980 more]"}}
		issues, err := goai.ExplainJSONSchemaValueWithResources(schema, json.RawMessage(`{"e":"v5000"}`), nil)
		if err != nil || !reflect.DeepEqual(issues, want) {
			t.Fatalf("diagnostics = %+v, %v; want %+v", issues, err, want)
		}
	})
	t.Run("L392-short-enum-record", func(t *testing.T) {
		assertV160MessageRecords(t, `{"enum":["a","b"]}`, `"c"`, `[{"path":"","keyword":"enum","message":"expected one of [\"a\",\"b\"]"}]`)
	})
	t.Run("L399-ten-thousand-members", func(t *testing.T) {
		members := make([]string, 10_000)
		for i := range members {
			members[i] = "member" + strconv.Itoa(i)
		}
		assertV160SchemaCheck(t, `{"enum":`+string(encodeV160Schema(t, members))+`}`, `"member9999"`, nil, "", false, true)
	})
}

// validator.test.ts:402–412. Every input is ["a"]; only the schema changes.
func TestSourceV160SchemaContainsMessages(t *testing.T) {
	for _, tc := range []struct{ name, keywords, want string }{
		{"L404-at-least-one", `"contains":{"type":"number"}`, "expected at least 1 matching item"},
		{"L405-at-least-two", `"contains":{"type":"number"},"minContains":2`, "expected at least 2 matching items"},
		{"L406-at-most-zero", `"contains":{"type":"string"},"maxContains":0,"minContains":0`, "expected at most 0 matching items"},
		{"L409-between-one-and-three", `"contains":{"type":"number"},"minContains":1,"maxContains":3`, "expected between 1 and 3 matching items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160MessageRecords(t, `{"type":"array",`+tc.keywords+`}`, `["a"]`,
				fmt.Sprintf(`[{"path":"","keyword":"contains","message":%q}]`, tc.want))
		})
	}
}

// validator.test.ts:414–441. The selected branch at each recursion depth
// is determined by the fields it declares in the corresponding object.
func TestSourceV160SchemaRecursiveOneOfMessages(t *testing.T) {
	const filter = `{
		"type":"object","properties":{"where":{"$ref":"#/$defs/F"}},
		"$defs":{"F":{"oneOf":[
			{"type":"object","properties":{"field":{"type":"string"},"eq":{"type":"string"}},"required":["field","eq"],"additionalProperties":false},
			{"type":"object","properties":{"and":{"type":"array","items":{"$ref":"#/$defs/F"}}},"required":["and"],"additionalProperties":false}
		]}}
	}`
	const value = `{"where":{"and":[{"field":"a","eq":1,"x":2}]}}`
	const want = `[{"path":"/where/and/0/eq","keyword":"type","message":"expected string, got integer"},{"path":"/where/and/0/x","keyword":"additionalProperties","message":"unknown field"}]`
	assertV160MessageRecords(t, filter, value, want)
}

func encodeV160Schema(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertV160MessageRecords(t *testing.T, schema, value, want string) {
	t.Helper()
	issues, err := goai.ExplainJSONSchemaValueWithResources(json.RawMessage(schema), json.RawMessage(value), nil)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, issues, want)
}
