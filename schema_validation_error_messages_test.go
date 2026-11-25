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

// validator.test.ts:384–400. Reuse the same prepared validator for checks
// and explanations where the pinned test holds a compiled schema.
func TestSourceV160SchemaEnumMessages(t *testing.T) {
	values := make([]string, 5000)
	for i := range values {
		values[i] = "v" + strconv.Itoa(i)
	}
	enum := encodeV160Schema(t, values)
	compiled := compileV160LeafSchema(t, `{"type":"object","properties":{"e":{"type":"string","enum":`+string(enum)+`}}}`)
	t.Run("L387-final-member", func(t *testing.T) {
		assertV160PreparedSchemaCheck(t, compiled, `{"e":"v4999"}`, true)
	})
	t.Run("L389-capped-enum-message", func(t *testing.T) {
		const invalid = `{"e":"v5000"}`
		assertV160PreparedSchemaCheck(t, compiled, invalid, false)
		listed := strings.TrimSuffix(string(encodeV160Schema(t, values[:20])), "]")
		want := []goai.JSONSchemaDiagnostic{{Path: "/e", Keyword: "enum", Message: "expected one of " + listed + ", ... and 4980 more]"}}
		issues, err := compiled.Explain(json.RawMessage(invalid))
		if err != nil || !reflect.DeepEqual(issues, want) {
			t.Fatalf("diagnostics = %+v, %v; want %+v", issues, err, want)
		}
	})
	t.Run("supplemental-first-member-after-rejection", func(t *testing.T) {
		assertV160PreparedSchemaCheck(t, compiled, `{"e":"v0"}`, true)
		assertV160PreparedMessageRecords(t, compiled, `{"e":"v0"}`, `[]`)
	})
	t.Run("L392-short-enum-record", func(t *testing.T) {
		short := compileV160LeafSchema(t, `{"enum":["a","b"]}`)
		assertV160PreparedMessageRecords(t, short, `"c"`, `[{"path":"","keyword":"enum","message":"expected one of [\"a\",\"b\"]"}]`)
	})
	t.Run("L399-ten-thousand-members", func(t *testing.T) {
		members := make([]string, 10_000)
		for i := range members {
			members[i] = "member" + strconv.Itoa(i)
		}
		large := compileV160LeafSchema(t, `{"enum":`+string(encodeV160Schema(t, members))+`}`)
		assertV160PreparedSchemaCheck(t, large, `"member10000"`, false)
		assertV160PreparedSchemaCheck(t, large, `"member9999"`, true)
		assertV160PreparedSchemaCheck(t, large, `"member10000"`, false)
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
			compiled := compileV160LeafSchema(t, `{"type":"array",`+tc.keywords+`}`)
			assertV160PreparedSchemaCheck(t, compiled, `["a"]`, false)
			assertV160PreparedMessageRecords(t, compiled, `["a"]`,
				fmt.Sprintf(`[{"path":"","keyword":"contains","message":%q}]`, tc.want))
		})
	}
}

func assertV160PreparedSchemaCheck(t *testing.T, compiled *goai.CompiledJSONSchema, value string, want bool) {
	t.Helper()
	got, err := compiled.Check(json.RawMessage(value))
	if err != nil || got != want {
		t.Fatalf("check(%s) = %t, %v; want %t", value, got, err, want)
	}
}

func assertV160PreparedMessageRecords(t *testing.T, compiled *goai.CompiledJSONSchema, value, want string) {
	t.Helper()
	issues, err := compiled.Explain(json.RawMessage(value))
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, issues, want)
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
