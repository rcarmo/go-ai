package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:97–111. Go maps have no prototype values to fall back to.
func TestSourceV160SchemaCheckOwnProperties(t *testing.T) {
	const own = `{"type":"object","properties":{"constructor":{"type":"string"},"toString":{"type":"number"}},"required":["constructor"],"additionalProperties":false}`
	for _, tc := range []struct {
		name, schema, value string
		want                bool
	}{
		{"L104-missing-constructor", own, `{}`, false},
		{"L105-own-constructor", own, `{"constructor":"a"}`, true},
		{"L106-toString-wrong-type", own, `{"constructor":"a","toString":"b"}`, false},
		{"L110-unknown-type", `{"type":"constructor"}`, `1`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160SchemaCheck(t, tc.schema, tc.value, nil, "", false, tc.want)
		})
	}
}

// validator.test.ts:142–147. As in the original, every input has the required
// string type; only the format option and the string's syntax differ.
func TestSourceV160SchemaCheckFormatOption(t *testing.T) {
	const email = `{"type":"string","format":"email"}`
	t.Run("L144-unconfigured-format-is-annotation", func(t *testing.T) {
		assertV160SchemaCheck(t, email, `"not an email"`, nil, "", false, true)
	})
	t.Run("L145-configured-format-rejects", func(t *testing.T) {
		assertV160SchemaCheck(t, email, `"not an email"`, nil, "", true, false)
	})
	t.Run("L146-configured-format-passes", func(t *testing.T) {
		assertV160SchemaCheck(t, email, `"a@example.com"`, nil, "", true, true)
	})
}

// validator.test.ts:149–157,164–168. The same referenced object is tested
// with a present and an absent property; the same string meets only draft 7.
func TestSourceV160SchemaCheckReferenceOptions(t *testing.T) {
	const point = `{"$ref":"https://example.com/point.json"}`
	resources := map[string]json.RawMessage{"https://example.com/point.json": json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}},"required":["x"]}`)}
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		{"L155-registered-reference-passes", `{"x":1}`, true},
		{"L156-registered-reference-rejects", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160SchemaCheck(t, point, tc.value, resources, "", false, tc.want)
		})
	}
	const siblings = `{"definitions":{"s":{"type":"string"}},"$ref":"#/definitions/s","maxLength":1}`
	t.Run("L166-draft7-ignores-siblings", func(t *testing.T) {
		assertV160SchemaCheck(t, siblings, `"ab"`, nil, goai.JSONSchemaReferenceDraft7, false, true)
	})
	t.Run("L167-default-checks-siblings", func(t *testing.T) {
		assertV160SchemaCheck(t, siblings, `"ab"`, nil, "", false, false)
	})
}

// Supplemental: the option follows references without leaking into other
// calls, and cannot disable regular schema constraints.
func TestSchemaCheckOptionsIsolation(t *testing.T) {
	const remote = "https://example.com/email.json"
	const schema = `{"type":"object","properties":{"address":{"$ref":"https://example.com/email.json"}},"required":["address"]}`
	resources := map[string]json.RawMessage{remote: json.RawMessage(`{"type":"string","format":"email","minLength":4}`)}
	assertV160SchemaCheck(t, schema, `{"address":"junk"}`, resources, "", false, true)
	assertV160SchemaCheck(t, schema, `{"address":"junk"}`, resources, "", true, false)
	assertV160SchemaCheck(t, schema, `{"address":"x"}`, resources, "", false, false)
	assertV160SchemaCheck(t, `{"type":"string","format":"not-built-in"}`, `"a"`, nil, "", true, true)

	email := decodeV160DiagnosticInput(t, `{"type":"string","format":"email"}`).(map[string]any)
	if goai.CheckJSONSchemaValue(email, "not an email") {
		t.Fatal("option changed the legacy standalone check")
	}
	tool := goai.Tool{Name: "email", Parameters: json.RawMessage(`{"type":"object","properties":{"address":{"type":"string","format":"email"}}}`)}
	value := map[string]any{"address": "junk"}
	if got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: value}); got != nil || err == nil {
		t.Fatalf("tool validation = %#v, %v; want format rejection", got, err)
	} else {
		assertV160DiagnosticLines(t, err, tool.Name, "  - address: expected format email")
	}
}

func assertV160SchemaCheck(t *testing.T, schema, value string, resources map[string]json.RawMessage, draft goai.JSONSchemaReferenceDraft, assertFormats, want bool) {
	t.Helper()
	options := goai.JSONSchemaCheckOptions{Resources: resources, Draft: draft, AssertFormats: assertFormats}
	got, err := goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), json.RawMessage(value), options)
	if err != nil || got != want {
		t.Fatalf("check(%s) = %t, %v; want %t", value, got, err, want)
	}
}
