package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:486–501. SchemaUndefined is an explicit native input;
// an absent map key and a present JSON null remain different values.
func TestSourceV160SchemaDeclaredUndefined(t *testing.T) {
	const leaf = `{"type":"undefined"}`
	const object = `{"type":"object","required":["selection","principalId"],"properties":{"selection":{"type":"string","const":"average"},"principalId":{"type":"undefined"}}}`
	for _, tc := range []struct {
		name, schema string
		value        any
		want         bool
	}{
		{"L487-typed-undefined", leaf, goai.SchemaUndefined{}, true},
		{"L488-null-is-not-undefined", leaf, nil, false},
		{"L491-present-undefined", object, map[string]any{"selection": "average", "principalId": goai.SchemaUndefined{}}, true},
		{"L492-missing-required", object, map[string]any{"selection": "average"}, false},
		{"L493-present-string", object, map[string]any{"selection": "average", "principalId": "x"}, false},
		{"L494-present-null", object, map[string]any{"selection": "average", "principalId": nil}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160UndefinedCheck(t, tc.schema, tc.value, tc.want)
		})
	}
	compiled := compileV160LeafSchema(t, object)
	for _, tc := range []struct{ name, raw, want string }{
		{"L495-required-diagnostic", `{"selection":"average"}`, `[{"path":"/principalId","keyword":"required","message":"missing"}]`},
		{"L498-type-diagnostic", `{"selection":"average","principalId":"x"}`, `[{"path":"/principalId","keyword":"type","message":"expected undefined, got string"}]`},
		{"explicit-null-diagnostic", `{"selection":"average","principalId":null}`, `[{"path":"/principalId","keyword":"type","message":"expected undefined, got null"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := compiled.Explain(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			assertV160StructuredDiagnostics(t, got, tc.want)
		})
	}
}

// Supplemental pinned ref and diagnostic boundaries, plus the native wire
// and tool policies: JSON and tool cloning do not create an undefined value.
func TestSchemaTypedUndefinedBoundaries(t *testing.T) {
	const referenced = `{"$defs":{"u":{"type":"undefined"}},"$ref":"#/$defs/u"}`
	undefined := goai.SchemaUndefined{}
	assertV160UndefinedCheck(t, referenced, undefined, true)
	assertV160UndefinedCheck(t, referenced, json.RawMessage(`null`), false)
	assertV160UndefinedCheck(t, referenced, json.RawMessage(`{}`), false)
	text := compileV160LeafSchema(t, `{"type":"string"}`)
	issues, err := text.Explain(undefined)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"type","message":"expected string, got undefined"}]`)
	if _, err := json.Marshal(undefined); err == nil {
		t.Fatal("undefined unexpectedly became JSON")
	}
	tool := goai.Tool{Name: "explicit", Parameters: json.RawMessage(`{"type":"object","properties":{"a":{"type":"undefined"}},"required":["a"]}`)}
	value, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]any{"a": undefined}})
	if value != nil || err == nil || err.Error() != "tool argument value cannot be cloned: goai.SchemaUndefined" {
		t.Fatalf("tool validation returned %#v, %v; want prior input-domain refusal", value, err)
	}
}

func assertV160UndefinedCheck(t *testing.T, schema string, value any, want bool) {
	t.Helper()
	got, err := compileV160LeafSchema(t, schema).Check(value)
	if err != nil || got != want {
		t.Errorf("compiled check(%T %#v) = %t, %v; want %t", value, value, got, err, want)
	}
	got, err = goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), value, goai.JSONSchemaCheckOptions{})
	if err != nil || got != want {
		t.Errorf("standalone check(%T %#v) = %t, %v; want %t", value, value, got, err, want)
	}
}
