package openairesponses

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func constrainedSamplingToolV160(parameters string, cfg *goai.ToolConstrainedSampling) goai.Tool {
	return goai.Tool{Name: "sample_tool", Description: "Sample tool", Parameters: json.RawMessage(parameters), ConstrainedSampling: cfg}
}

// Source: constrained-sampling.test.ts:92-124. Explicit JavaScript false is
// not a value of the native pointer field; the false-versus-omission source
// assertion on line 126 is not claimed here.
func TestSourceV160ResponsesSupportedConstraints(t *testing.T) {
	const parameters = `{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"],"additionalProperties":false}`
	strict := constrainedSamplingToolV160(parameters, &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"})
	def, err := convertResponsesTool(strict, true, false)
	if err != nil || def.Type != "function" || def.Name != "sample_tool" || def.Strict == nil || !*def.Strict {
		t.Fatalf("strict conversion got %+v, err=%v", def, err)
	}
	strict.ConstrainedSampling.Strict = "require"
	if _, err := convertResponsesTool(strict, false, false); err == nil || !strings.Contains(err.Error(), `Tool "sample_tool" requires JSON-schema constrained sampling`) {
		t.Errorf("required strict diagnostic=%v", err)
	}
	grammar := constrainedSamplingToolV160(parameters, &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /[a-z]+/"}})
	custom, err := convertResponsesTool(grammar, true, true)
	if err != nil || custom.Type != "custom" || custom.Name != "sample_tool" || !reflect.DeepEqual(custom.Format, map[string]any{
		"type": "grammar", "syntax": "lark", "definition": "start: /[a-z]+/",
	}) {
		t.Fatalf("grammar conversion got %+v, err=%v", custom, err)
	}
	invalid := constrainedSamplingToolV160(parameters, &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{}})
	if _, err := convertResponsesTool(invalid, true, true); err == nil || !strings.Contains(err.Error(), `Tool "sample_tool" cannot use grammar constrained sampling: no supported grammar variant was provided`) {
		t.Errorf("invalid grammar diagnostic=%v", err)
	}
	fallback, err := convertResponsesTool(grammar, false, false)
	if err != nil || fallback.Type != "function" || fallback.Name != "sample_tool" {
		t.Fatalf("unsupported grammar fallback got %+v, err=%v", fallback, err)
	}
	if value := schemaObjectV160(t, marshalToolDefV160(t, fallback)); len(value) == 0 {
		t.Fatal("missing fallback tool")
	} else if _, present := value["strict"]; present {
		t.Fatalf("unsupported strict field must be omitted, not null or false: %#v", value)
	}
}

func marshalToolDefV160(t *testing.T, def toolDef) []byte {
	t.Helper()
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func schemaObjectV160(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

// Source: constrained-sampling.test.ts:131-157. Check required as an ordered
// array, including nested optional properties, without sorting the oracle.
func TestSourceV160ResponsesStrictSchemaProjection(t *testing.T) {
	parameters := json.RawMessage(`{
  "type":"object",
  "properties":{
    "path":{"type":"string"},
    "offset":{"type":"number"},
    "metadata":{"type":"object","properties":{"enabled":{"type":"boolean"}}},
    "nullable":{"anyOf":[{"type":"string"},{"type":"null"}]}
  },
  "required":["path","metadata"]
}`)
	original := bytes.Clone(parameters)
	strict, err := goai.MakeStrictJSONSchema(parameters)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(parameters, original) {
		t.Fatal("strict conversion mutated the tool schema")
	}
	base := schemaObjectV160(t, parameters)
	if _, present := base["additionalProperties"]; present || !reflect.DeepEqual(base["required"], []any{"path", "metadata"}) {
		t.Fatalf("native source schema changed: %#v", base)
	}
	want := schemaObjectV160(t, []byte(`{
  "type":"object",
  "properties":{
    "path":{"type":"string"},
    "offset":{"anyOf":[{"type":"number"},{"type":"null"}]},
    "metadata":{"type":"object","additionalProperties":false,"required":["enabled"],"properties":{"enabled":{"anyOf":[{"type":"boolean"},{"type":"null"}]}}},
    "nullable":{"anyOf":[{"type":"string"},{"type":"null"}]}
  },
  "additionalProperties":false,
  "required":["path","offset","metadata","nullable"]
}`))
	if got := schemaObjectV160(t, strict); !reflect.DeepEqual(got, want) {
		t.Fatalf("ordered strict schema got %s", strict)
	}
}

// Source: constrained-sampling.test.ts:159-165.
func TestSourceV160ResponsesStrictEnumWithoutType(t *testing.T) {
	const parameters = `{"type":"object","properties":{"level":{"enum":["low","high"]}},"required":["level"]}`
	tool := constrainedSamplingToolV160(parameters, &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"})
	level := schemaObjectV160(t, tool.Parameters)["properties"].(map[string]any)["level"].(map[string]any)
	if _, typed := level["type"]; typed {
		t.Fatal("fixture must be an enum without type")
	}
	def, err := convertResponsesTool(tool, true, false)
	if err != nil || def.Strict == nil || !*def.Strict {
		t.Fatalf("wanted strict=true for a bare enum, got %+v, err=%v", def, err)
	}
}

// Source: constrained-sampling.test.ts:167-227. Keep the seven cases in source
// order. Native error matching uses the same substring as Vitest's toThrow.
func TestSourceV160ResponsesStrictSchemaRefusal(t *testing.T) {
	for _, tc := range []struct{ name, parameters, diagnostic string }{
		{"schema additionalProperties", `{"type":"object","properties":{"metadata":{"type":"object","properties":{},"additionalProperties":{"type":"string"}}},"required":["metadata"]}`, "additionalProperties is unsupported"},
		{"allOf", `{"type":"object","allOf":[{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]},{"type":"object","properties":{"b":{"type":"number"}},"required":["b"]}]}`, "allOf schemas are unsupported"},
		{"union with object", `{"type":"object","properties":{"value":{"anyOf":[{"type":"object","properties":{"nested":{"type":"string"}},"required":["nested"]},{"type":"null"}]}},"required":["value"]}`, "object and array unions are unsupported"},
		{"external ref", `{"type":"object","properties":{"child":{"$ref":"https://example.com/child.json"}},"required":["child"]}`, "$ref schemas are unsupported"},
		{"untyped array items", `{"type":"object","properties":{"commandArguments":{"type":"array","items":{}}}}`, "schemas without a type are unsupported"},
		{"untyped property", `{"type":"object","properties":{"value":{"description":"Any JSON value"}},"required":["value"]}`, "schemas without a type are unsupported"},
		{"bare const", `{"type":"object","properties":{"mode":{"const":"fast"}},"required":["mode"]}`, "schemas without a type are unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool := constrainedSamplingToolV160(tc.parameters, &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"})
			if _, err := goai.MakeStrictJSONSchema(tool.Parameters); err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Errorf("strict conversion error=%v, want %q", err, tc.diagnostic)
			}
			if resolved, err := goai.ResolveJSONSchemaStrictSampling(tool, true); resolved != nil || err != nil {
				t.Errorf("prefer resolution=%v, err=%v, want absent", resolved, err)
			}
			def, err := convertResponsesTool(tool, true, false)
			if err != nil || def.Strict == nil || *def.Strict || !reflect.DeepEqual(schemaObjectV160(t, def.Parameters), schemaObjectV160(t, tool.Parameters)) {
				t.Errorf("prefer fallback strict=%v parameters=%s err=%v, want false and original parameters", def.Strict, def.Parameters, err)
			}
			tool.ConstrainedSampling.Strict = "require"
			if _, err := goai.ResolveJSONSchemaStrictSampling(tool, true); err == nil || !strings.Contains(err.Error(), tc.diagnostic) {
				t.Errorf("require error=%v, want %q", err, tc.diagnostic)
			}
		})
	}
}

// Supplemental ordering corner independent of the named TypeBox fixture:
// Object.keys sorts canonical array indices, not arbitrary numeric strings.
func TestStrictSchemaJSPropertyOrderV160(t *testing.T) {
	input := json.RawMessage(`{"type":"object","properties":{"z":{"type":"string"},"10":{"type":"string"},"4294967295":{"type":"string"},"2":{"type":"string"},"01":{"type":"string"},"0":{"type":"string"},"4294967294":{"type":"string"},"a":{"type":"string"}}}`)
	strict, err := goai.MakeStrictJSONSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	got := schemaObjectV160(t, strict)["required"]
	want := []any{"0", "2", "10", "4294967294", "z", "4294967295", "01", "a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required order got %v, want %v", got, want)
	}
}
