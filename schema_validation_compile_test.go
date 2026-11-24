package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const v160DanglingSchema = `{"type":"object","properties":{"t":{"$ref":"#/definitions/Missing"}}}`
const v160MissingHostSchema = `{"type":"object","properties":{"t":{"$ref":"https://example.com/schemas/t.json"}}}`

// validator.test.ts:159–162 and 283–289. No input is passed to the constructor,
// including for the references in optional properties.
func TestSourceV160SchemaCompileUnresolvedReferences(t *testing.T) {
	for _, tc := range []struct{ name, schema, ref string }{
		{"L160-root-host", `{"$ref":"https://example.com/missing.json"}`, "https://example.com/missing.json"},
		{"L161-root-local", `{"$ref":"#/$defs/missing"}`, "#/$defs/missing"},
		{"L287-optional-local", v160DanglingSchema, "#/definitions/Missing"},
		{"L288-optional-host", v160MissingHostSchema, "https://example.com/schemas/t.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := goai.CompileJSONSchema(json.RawMessage(tc.schema), goai.JSONSchemaCompileOptions{})
			if compiled != nil || err == nil || !strings.Contains(err.Error(), "Unresolved $ref "+tc.ref) {
				t.Fatalf("compile returned validator=%t, error=%v; want unresolved $ref %s", compiled != nil, err, tc.ref)
			}
		})
	}
}

// validator.test.ts:291–300. Reject mode prepares the schema and rejects only
// reached values. Reusing the validator cannot retain a previous verdict.
func TestSourceV160SchemaCompileRejectAtValue(t *testing.T) {
	for _, tc := range []struct{ name, schema, diagnostic string }{
		{"local", v160DanglingSchema, `[{"path":"/t","keyword":"$ref","message":"unresolved reference #/definitions/Missing"}]`},
		{"host", v160MissingHostSchema, `[{"path":"/t","keyword":"$ref","message":"unresolved reference https://example.com/schemas/t.json"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := goai.CompileJSONSchema(json.RawMessage(tc.schema), goai.JSONSchemaCompileOptions{RejectUnresolvedRefs: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				name, value string
				want        bool
			}{
				{"L294-optional-omission", `{}`, true},
				{"L295-present-value", `{"t":1}`, false},
				{"reuse-optional-omission", `{}`, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got, err := compiled.Check(json.RawMessage(tc.value))
					if err != nil || got != tc.want {
						t.Fatalf("check(%s) = %t, %v; want %t", tc.value, got, err, tc.want)
					}
				})
			}
			issues, err := compiled.Explain(json.RawMessage(`{"t":1}`))
			if err != nil {
				t.Fatal(err)
			}
			assertV160StructuredDiagnostics(t, issues, tc.diagnostic)
		})
	}
}

// Supplemental reachability cases verified through the pinned compiler. The
// default must find an unresolved edge before a value is ever supplied, but
// inactive definitions, unvisited documents and ignored siblings are allowed.
func TestSchemaCompileReachability(t *testing.T) {
	const host = "https://example.com/host.json"
	const missing = "https://example.com/missing.json"
	const withHost = `{"$ref":"https://example.com/host.json"}`
	const withSibling = `{"$ref":"https://example.com/host.json","properties":{"p":{"$ref":"https://example.com/missing.json"}}}`
	unused := map[string]json.RawMessage{host: json.RawMessage(`{"$ref":"https://example.com/missing.json"}`)}
	object := map[string]json.RawMessage{host: json.RawMessage(`{"type":"object"}`)}
	for _, tc := range []struct {
		name, schema string
		resources    map[string]json.RawMessage
		draft        goai.JSONSchemaReferenceDraft
		missing      string
	}{
		{"unused-definition", `{"$defs":{"dormant":{"$ref":"https://example.com/missing.json"}},"type":"object"}`, nil, "", ""},
		{"unused-host", `{"type":"object"}`, unused, "", ""},
		{"reached-host", withHost, unused, "", missing},
		{"draft7-ignores-siblings", withSibling, object, goai.JSONSchemaReferenceDraft7, ""},
		{"default-follows-siblings", withSibling, object, "", missing},
		{"recursive-root", `{"type":"object","properties":{"child":{"$ref":"#"}}}`, nil, "", ""},
		{"dynamic-recursive-root", `{"$dynamicAnchor":"node","type":"object","properties":{"child":{"$dynamicRef":"#node"}}}`, nil, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{Resources: tc.resources, Draft: tc.draft}}
			compiled, err := goai.CompileJSONSchema(json.RawMessage(tc.schema), options)
			if tc.missing != "" {
				if compiled != nil || err == nil || !strings.Contains(err.Error(), "Unresolved $ref "+tc.missing) {
					t.Fatalf("compile returned validator=%t, error=%v; want unresolved $ref %s", compiled != nil, err, tc.missing)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if accepted, err := compiled.Check(json.RawMessage(`{}`)); !accepted || err != nil {
				t.Fatalf("check({}) = %t, %v; want true", accepted, err)
			}
		})
	}
}

func TestSchemaCompileOwnsCanonicalRegistry(t *testing.T) {
	const host = "https://example.com/schemas/nodes/point.json"
	schema := json.RawMessage(`{"$id":"https://example.com/schemas/root.json","$ref":"nodes/point.json"}`)
	point := json.RawMessage(`{"$id":"point.json","type":"object","properties":{"n":{"$ref":"#/$defs/count"}},"required":["n"],"$defs":{"count":{"type":"integer"}}}`)
	resources := map[string]json.RawMessage{host: point}
	options := goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{Resources: resources}}
	compiled, err := goai.CompileJSONSchema(schema, options)
	if err != nil {
		t.Fatal(err)
	}
	clear(schema)
	clear(point)
	delete(resources, host)
	for range 2 {
		if accepted, err := compiled.Check(json.RawMessage(`{"n":1}`)); !accepted || err != nil {
			t.Fatalf("check valid point = %t, %v", accepted, err)
		}
		issues, err := compiled.Explain(json.RawMessage(`{"n":"invalid"}`))
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/n","keyword":"type","message":"expected integer, got string"}]`)
	}
	if accepted, err := goai.CheckJSONSchemaValueWithOptions(json.RawMessage(v160DanglingSchema), json.RawMessage(`{}`), goai.JSONSchemaCheckOptions{}); !accepted || err != nil {
		t.Fatalf("legacy check of omitted optional field = %t, %v", accepted, err)
	}
	tool := goai.Tool{Name: "lazy", Parameters: json.RawMessage(v160DanglingSchema)}
	got, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	assertV160StandaloneJSON(t, got, `{}`)
}

// The first host document is visible during preparation. The second is
// reached only when a dynamic anchor in the caller's resource wins at runtime.
func TestSchemaCompileKeepsDynamicRegistryLazy(t *testing.T) {
	const base = "https://example.com/schemas/"
	root := json.RawMessage(`{
		"$id":"https://example.com/schemas/root.json","$ref":"list.json",
		"$defs":{"item":{"$dynamicAnchor":"item","$ref":"string.json"}}
	}`)
	resources := map[string]json.RawMessage{
		base + "list.json":   json.RawMessage(`{"type":"array","items":{"$dynamicRef":"#item"},"$defs":{"item":{"$dynamicAnchor":"item"}}}`),
		base + "string.json": json.RawMessage(`{"type":"string"}`),
	}
	compiled, err := goai.CompileJSONSchema(root, goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{Resources: resources}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input string
		want  bool
	}{
		{`["a"]`, true}, {`[1]`, false}, {`["b"]`, true},
	} {
		got, err := compiled.Check(json.RawMessage(tc.input))
		if err != nil || got != tc.want {
			t.Fatalf("check(%s) = %t, %v; want %t", tc.input, got, err, tc.want)
		}
	}
}
