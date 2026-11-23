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

// Exact path, keyword, message, count and order from the five errors cases in
// packages/schema/test/validator.test.ts. Check is a separate public call.
func TestSourceV160SchemaStructuredDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, schema, input, diagnostics string
	}{
		{"L35-missing-field-and-unknown-field", v160ReadDiagnosticsSchema, `{"offset":0,"extra":true}`,
			`[{"path":"/path","keyword":"required","message":"missing"},{"path":"/offset","keyword":"minimum","message":"expected >= 1"},{"path":"/extra","keyword":"additionalProperties","message":"unknown field"}]`},
		{"L43-empty-for-valid-value", v160ReadDiagnosticsSchema, `{"path":"a.ts"}`, `[]`},
		{"L48-selected-tag", v160TaggedDiagnosticsSchema, `[{"kind":"text","text":"a"},{"kind":"image","width":-1}]`,
			`[{"path":"/1/width","keyword":"minimum","message":"expected >= 0"}]`},
		{"L54-unknown-tag", v160TaggedDiagnosticsSchema, `[{"kind":"video"}]`,
			`[{"path":"/0/kind","keyword":"anyOf","message":"expected one of [\"text\",\"image\"]"}]`},
		{"L61-pointer-escaping", v160PointerDiagnosticsSchema, `{"a/b~c":1}`,
			`[{"path":"/a~1b~0c","keyword":"type","message":"expected string, got integer"}]`},
		{"L72-referenced-schema", v160RefDiagnosticsSchema, `[{"x":1},{}]`,
			`[{"path":"/1/x","keyword":"required","message":"missing"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := decodeV160DiagnosticInput(t, tc.schema).(map[string]any)
			value := decodeV160DiagnosticInput(t, tc.input)
			if accepted := goai.CheckJSONSchemaValue(schema, value); accepted != (tc.diagnostics == `[]`) {
				t.Fatalf("strict check = %t; want %t", accepted, tc.diagnostics == `[]`)
			}
			issues, err := goai.ExplainJSONSchemaValueWithResources(json.RawMessage(tc.schema), value, nil)
			if err != nil {
				t.Fatal(err)
			}
			assertV160StructuredDiagnostics(t, issues, tc.diagnostics)
		})
	}
}

func assertV160StructuredDiagnostics(t *testing.T, got any, want string) {
	t.Helper()
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(got); err != nil {
		t.Fatal(err)
	}
	if text := strings.TrimSuffix(encoded.String(), "\n"); text != want {
		t.Fatalf("diagnostics = %s; want %s", text, want)
	}
}

// Supplemental Go boundary cases: host references, declaration order, absence
// of implicit fetching, strict draft policies and errors from schema setup.
func TestSourceV160SchemaStructuredDiagnosticResources(t *testing.T) {
	const uri = "https://example.com/point.json"
	root := json.RawMessage(`{"type":"object","properties":{"z":{"$ref":"https://example.com/point.json"},"a":{"type":"integer"}}}`)
	point := json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)
	value := decodeV160DiagnosticInput(t, `{"a":"2","z":{"n":"1"}}`)
	resources := map[string]json.RawMessage{uri: point}
	got, err := goai.ExplainJSONSchemaValueWithResources(root, value, resources)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, got, `[{"path":"/z/n","keyword":"type","message":"expected integer, got string"},{"path":"/a","keyword":"type","message":"expected integer, got string"}]`)
	got, err = goai.ExplainJSONSchemaValueWithResources(root, value, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, got, `[{"path":"/z","keyword":"$ref","message":"unresolved reference https://example.com/point.json"},{"path":"/a","keyword":"type","message":"expected integer, got string"}]`)

	draftRoot := json.RawMessage(`{"$ref":"https://example.com/point.json","maxProperties":0}`)
	passing := decodeV160DiagnosticInput(t, `{"n":1}`)
	got, err = goai.ExplainJSONSchemaValueWithResources(draftRoot, passing, resources, goai.JSONSchemaReferenceDraft7)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, got, `[]`)
	got, err = goai.ExplainJSONSchemaValueWithResources(draftRoot, passing, resources)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, got, `[{"path":"","keyword":"maxProperties","message":"expected at most 0 properties"}]`)

	got, err = goai.ExplainJSONSchemaValueWithResources(root, value, map[string]json.RawMessage{uri: json.RawMessage(`42`)})
	if got != nil || err == nil || err.Error() != `schema resource "https://example.com/point.json": expected an object or boolean` {
		t.Fatalf("invalid host resource = %#v, %v", got, err)
	}
}

func TestSourceV160SchemaStructuredDiagnosticRoot(t *testing.T) {
	got, err := goai.ExplainJSONSchemaValueWithResources(json.RawMessage(`false`), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, got, `[{"path":"","keyword":"false","message":"no value is allowed here"}]`)

	got, err = goai.ExplainJSONSchemaValueWithResources(json.RawMessage(`{"properties":{"":{"type":"integer"}}}`), map[string]any{"": "s"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, got, `[{"path":"/","keyword":"type","message":"expected integer, got string"}]`)
}

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
