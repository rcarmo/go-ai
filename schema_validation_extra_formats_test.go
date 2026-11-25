package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const v160PointerFragmentSchema = `{"type":"string","format":"json-pointer-uri-fragment"}`

// validator.test.ts:458–466. Both standalone check paths opt into the
// built-in format checks; URL and pointer-fragment syntax are distinct.
func TestSourceV160SchemaExtraFormats(t *testing.T) {
	const url = `{"type":"string","format":"url"}`
	options := goai.JSONSchemaCheckOptions{AssertFormats: true}
	for _, tc := range []struct {
		name, schema, value string
		want                bool
	}{
		{"L460-url-valid", url, "https://example.com/a?b=1", true},
		{"L461-url-invalid", url, "not a url", false},
		{"L463-fragment-valid", v160PointerFragmentSchema, "#/a/b~1c", true},
		{"L464-fragment-raw-space", v160PointerFragmentSchema, "#/a b", false},
		{"L465-pointer-without-fragment-prefix", v160PointerFragmentSchema, "/a", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := goai.CompileJSONSchema(json.RawMessage(tc.schema), goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: options})
			if err != nil {
				t.Fatal(err)
			}
			got, err := compiled.Check(tc.value)
			if err != nil || got != tc.want {
				t.Errorf("compiled check(%q) = %t, %v; want %t", tc.value, got, err, tc.want)
			}
			got, err = goai.CheckJSONSchemaValueWithOptions(json.RawMessage(tc.schema), tc.value, options)
			if err != nil || got != tc.want {
				t.Errorf("standalone check(%q) = %t, %v; want %t", tc.value, got, err, tc.want)
			}
		})
	}
}

// Supplemental boundaries captured from the unchanged pinned formatter.
func TestSchemaPointerFragmentFormatBoundaries(t *testing.T) {
	options := goai.JSONSchemaCheckOptions{AssertFormats: true}
	compiled, err := goai.CompileJSONSchema(json.RawMessage(v160PointerFragmentSchema), goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"#", true}, {"#/", true}, {"#/a~0b", true}, {"#/%20", true}, {"#/%AF", true}, {"#/%ff", true},
		{"#/%2", false}, {"#/%zz", false}, {"#/~2", false}, {"#/?", false}, {"#/ä", false}, {"#/a\n", false}, {"#/K", false}, {"#/ſ", false},
	} {
		got, err := compiled.Check(tc.value)
		if err != nil || got != tc.want {
			t.Errorf("compiled check(%q) = %t, %v; want %t", tc.value, got, err, tc.want)
		}
	}
	issues, err := compiled.Explain("#/a b")
	if err != nil {
		t.Fatal(err)
	}
	assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"format","message":"expected format json-pointer-uri-fragment"}]`)
}

func TestSchemaPointerFragmentFormatPolicy(t *testing.T) {
	const invalid = "#/a b"
	schema := json.RawMessage(v160PointerFragmentSchema)
	compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := compiled.Check(invalid); err != nil || !got {
		t.Fatalf("default compiled check = %t, %v; want annotation", got, err)
	}
	if got, err := goai.CheckJSONSchemaValueWithOptions(schema, invalid, goai.JSONSchemaCheckOptions{}); err != nil || !got {
		t.Fatalf("default standalone check = %t, %v; want annotation", got, err)
	}
	legacy := decodeV160DiagnosticInput(t, v160PointerFragmentSchema).(map[string]any)
	if !goai.CheckJSONSchemaValue(legacy, invalid) {
		t.Fatal("legacy check now asserts the new format")
	}
	tool := goai.Tool{Name: "pointer", Parameters: json.RawMessage(`{"type":"object","properties":{"p":` + v160PointerFragmentSchema + `}}`)}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]any{"p": invalid}})
	if err != nil {
		t.Fatal(err)
	}
	assertV160StandaloneJSON(t, got, `{"p":"#/a b"}`)
}
