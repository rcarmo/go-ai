package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const v160ReadDiagnosticsSchema = `{
	"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer","minimum":1}},
	"required":["path"],"additionalProperties":false
}`

const v160TaggedDiagnosticsSchema = `{
	"type":"array","items":{"anyOf":[
		{"type":"object","properties":{"kind":{"const":"text"},"text":{"type":"string"}},"required":["kind","text"],"additionalProperties":false},
		{"type":"object","properties":{"kind":{"const":"image"},"width":{"type":"integer","minimum":0}},"required":["kind","width"],"additionalProperties":false}
	]}
}`

const v160RefDiagnosticsSchema = `{
	"$defs":{"point":{"type":"object","properties":{"x":{"type":"number"}},"required":["x"]}},
	"type":"array","items":{"$ref":"#/$defs/point"}
}`

const v160PointerDiagnosticsSchema = `{"type":"object","properties":{"a/b~c":{"type":"string"}}}`

// The tool boundary adds a dotted path, omits keywords and coerces before it
// checks, unlike the validator.errors calls in packages/schema/test/validator.test.ts.
func TestSourceV160SchemaToolDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, schema, input, diagnostics string
	}{
		{"L35-missing-field-and-unknown-field", v160ReadDiagnosticsSchema,
			`{"offset":0,"extra":true}`, "  - path: missing\n  - offset: expected >= 1\n  - extra: unknown field"},
		{"L43-empty-for-valid-value", v160ReadDiagnosticsSchema, `{"path":"a.ts"}`, ""},
		{"L49-selected-tag", v160TaggedDiagnosticsSchema,
			`[{"kind":"text","text":"a"},{"kind":"image","width":-1}]`, "  - 1.width: expected >= 0"},
		{"L54-unknown-tag", v160TaggedDiagnosticsSchema,
			`[{"kind":"video"}]`, `  - 0.kind: expected one of ["text","image"]`},
		{"L72-referenced-schema", v160RefDiagnosticsSchema, `[{"x":1},{}]`, "  - 1.x: missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := decodeV160DiagnosticInput(t, tc.input)
			tool := goai.Tool{Name: "diagnostics", Parameters: json.RawMessage(tc.schema)}
			got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: input})
			if tc.diagnostics == "" {
				encoded, encodeErr := json.Marshal(got)
				if err != nil || encodeErr != nil || string(encoded) != tc.input {
					t.Fatalf("tool result = %s, %v, %v; want %s", encoded, err, encodeErr, tc.input)
				}
				return
			}
			if got != nil || err == nil {
				t.Fatalf("tool result = %#v, %v; want rejection", got, err)
			}
			assertV160DiagnosticLines(t, err, tool.Name, tc.diagnostics)
		})
	}
}

// validator.test.ts:L61, through the existing strict public check and
// first-diagnostic APIs. The native error gives a plain key, not a pointer.
func TestSourceV160SchemaStrictDiagnosticPath(t *testing.T) {
	schema := decodeV160DiagnosticInput(t, v160PointerDiagnosticsSchema).(map[string]any)
	value := decodeV160DiagnosticInput(t, `{"a/b~c":1}`)
	if goai.CheckJSONSchemaValue(schema, value) {
		t.Fatal("accepted integer for a string property")
	}
	const want = "a/b~c: expected string, got integer"
	if err := goai.ValidateJSONSchemaValueWithResources(schema, value, nil); err == nil || err.Error() != want {
		t.Fatalf("first diagnostic = %v; want %q", err, want)
	}
}

// coerce.test.ts:L414–437: preserve all 42 errors in recursive branch order.
func TestSourceV160SchemaRecursiveDiagnosticLines(t *testing.T) {
	const schema = `{
		"type":"object","properties":{"s":{"$ref":"#/$defs/S"}},
		"$defs":{"S":{"anyOf":[
			{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["a"]},
			{"type":"object","additionalProperties":{"$ref":"#/$defs/S"},"required":["b"]}
		]}}
	}`
	var value any = map[string]any{"x": 1}
	var want []string
	for i := range 41 {
		want = append(want, "  - s"+strings.Repeat(".c", i)+".a: missing")
		if i < 40 {
			value = map[string]any{"c": value}
		}
	}
	want = append(want, "  - s"+strings.Repeat(".c", 40)+".x: expected object, got integer")
	tool := goai.Tool{Name: "recursive", Parameters: json.RawMessage(schema)}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]any{"s": value}})
	if got != nil || err == nil {
		t.Fatalf("tool result = %#v, %v; want rejection", got, err)
	}
	assertV160DiagnosticLines(t, err, tool.Name, strings.Join(want, "\n"))
}

// packages/ai/test/validation.test.ts:L211–217 exercises the actual public
// formatting boundary rather than translating the standalone schema API.
func TestSourceV160SchemaToolErrorEnvelope(t *testing.T) {
	tool := goai.Tool{Name: "echo", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"number"}},"required":["value"]}`)}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]any{"value": "many"}})
	const want = "Validation failed for tool \"echo\":\n  - value: expected number, got string\n\nReceived arguments:\n{\n  \"value\": \"many\"\n}"
	if got != nil || err == nil || err.Error() != want {
		t.Fatalf("tool result = %#v, %v; want %q", got, err, want)
	}
}

func assertV160DiagnosticLines(t *testing.T, err error, tool, want string) {
	t.Helper()
	prefix := "Validation failed for tool " + `"` + tool + `"` + ":\n"
	section, found := strings.CutPrefix(err.Error(), prefix)
	diagnostics, _, split := strings.Cut(section, "\n\nReceived arguments:\n")
	if !found || !split || diagnostics != want {
		t.Fatalf("tool diagnostics = %q; want %q", diagnostics, want)
	}
}

func decodeV160DiagnosticInput(t *testing.T, text string) any {
	t.Helper()
	var value any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
