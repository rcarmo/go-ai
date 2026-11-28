package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Locator: packages/schema/test/coerce.test.ts, "member keywords across the
// passes" and the anyOf/oneOf node-candidate cases. Git blob
// b264a442d9105ed53f1a3bb88dfd8a404886da7a; the cases are translation-stable
// JSON schemas and inputs with no host or clock dependency.
func TestSchemaCompiledMemberKeywordsStrictAndCoerced(t *testing.T) {
	for _, tc := range []struct {
		name        string
		schema      json.RawMessage
		input       json.RawMessage
		coerced     json.RawMessage
		diagnostics string
	}{
		{
			name:    "pattern-and-additional-members",
			schema:  json.RawMessage(`{"type":"object","patternProperties":{"^n_":{"type":["integer","null"]}},"additionalProperties":{"type":"string"}}`),
			input:   json.RawMessage(`{"n_a":"5","other":7}`),
			coerced: json.RawMessage(`{"n_a":5,"other":"7"}`),
			diagnostics: `[{"path":"/n_a","keyword":"type","message":"expected integer or null, got string"},` +
				`{"path":"/other","keyword":"type","message":"expected string, got integer"}]`,
		},
		{
			name:        "properties-beside-anyOf",
			schema:      json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer"},"b":{"type":"integer"}},"anyOf":[{"required":["a"]},{"required":["b"]}]}`),
			input:       json.RawMessage(`{"a":"3.5"}`),
			coerced:     json.RawMessage(`{"a":3}`),
			diagnostics: `[{"path":"/a","keyword":"type","message":"expected integer, got string"}]`,
		},
		{
			name:    "anyOf-candidate-after-node-properties",
			schema:  json.RawMessage(`{"type":"object","anyOf":[{"type":"object","properties":{"a":{"type":"array","items":{"type":"integer"}}},"required":["a"]},{"type":"object","properties":{"b":{"type":"boolean"}},"required":["b"]}],"properties":{"c":{"type":"integer"}},"required":["c"]}`),
			input:   json.RawMessage(`{"a":"3","c":"2"}`),
			coerced: json.RawMessage(`{"a":[3],"c":2}`),
			diagnostics: `[{"path":"/c","keyword":"type","message":"expected integer, got string"},` +
				`{"path":"/a","keyword":"type","message":"expected array, got string"}]`,
		},
		{
			name:    "oneOf-candidate-after-node-properties",
			schema:  json.RawMessage(`{"type":"object","oneOf":[{"type":"object","properties":{"a":{"type":"array","items":{"type":"integer"}}},"required":["a"]},{"type":"object","properties":{"b":{"type":"boolean"}},"required":["b"]}],"properties":{"c":{"type":"integer"}},"required":["c"]}`),
			input:   json.RawMessage(`{"a":"3","c":"2"}`),
			coerced: json.RawMessage(`{"a":[3],"c":2}`),
			diagnostics: `[{"path":"/c","keyword":"type","message":"expected integer, got string"},` +
				`{"path":"/a","keyword":"type","message":"expected array, got string"}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := goai.CompileJSONSchema(tc.schema, goai.JSONSchemaCompileOptions{})
			if err != nil {
				t.Fatal(err)
			}

			var input, want any
			if err := json.Unmarshal(tc.input, &input); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(tc.coerced, &want); err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}

			ok, err := compiled.Check(input)
			if err != nil || ok {
				t.Fatalf("strict check = %t, %v; want false", ok, err)
			}
			issues, err := compiled.Explain(input)
			if err != nil {
				t.Fatal(err)
			}
			assertSchemaMemberDiagnostics(t, issues, tc.diagnostics)

			got, err := compiled.Coerce(input)
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			wantJSON, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("coerced = %s; want %s", gotJSON, wantJSON)
			}
			parsed, parseIssues, err := compiled.Parse(input)
			if err != nil {
				t.Fatal(err)
			}
			parsedJSON, err := json.Marshal(parsed)
			if err != nil {
				t.Fatal(err)
			}
			if string(parsedJSON) != string(wantJSON) {
				t.Fatalf("parsed = %s; want %s", parsedJSON, wantJSON)
			}
			assertSchemaMemberDiagnostics(t, parseIssues, `[]`)

			ok, err = compiled.Check(got)
			if err != nil || !ok {
				t.Fatalf("coerced check = %t, %v; want true", ok, err)
			}
			issues, err = compiled.Explain(got)
			if err != nil {
				t.Fatal(err)
			}
			assertSchemaMemberDiagnostics(t, issues, `[]`)

			after, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatalf("coerce mutated input: before %s, after %s", before, after)
			}
		})
	}
}

func assertSchemaMemberDiagnostics(t *testing.T, got any, want string) {
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
