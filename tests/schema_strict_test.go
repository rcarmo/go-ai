package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMakeStrictJSONSchemaRequiresOptionalNullableProperties(t *testing.T) {
	parameters := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"number"},"metadata":{"type":"object","properties":{"enabled":{"type":"boolean"}}},"nullable":{"anyOf":[{"type":"string"},{"type":"null"}]}},"required":["path","metadata"]}`)
	strict, err := goai.MakeStrictJSONSchema(parameters)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(strict, &got); err != nil {
		t.Fatal(err)
	}
	if got["additionalProperties"] != false {
		t.Fatalf("expected additionalProperties false, got %#v", got["additionalProperties"])
	}
	required := got["required"].([]interface{})
	if strings.Join([]string{required[0].(string), required[1].(string), required[2].(string), required[3].(string)}, ",") != "metadata,nullable,offset,path" {
		t.Fatalf("unexpected required list: %#v", required)
	}
	props := got["properties"].(map[string]interface{})
	offset := props["offset"].(map[string]interface{})
	if _, ok := offset["anyOf"].([]interface{}); !ok {
		t.Fatalf("offset was not widened to nullable anyOf: %#v", offset)
	}
	metadata := props["metadata"].(map[string]interface{})
	if metadata["additionalProperties"] != false {
		t.Fatalf("nested object not strict: %#v", metadata)
	}
}

func TestResolveJSONSchemaStrictSamplingFallsBackOrRequires(t *testing.T) {
	tool := goai.Tool{Name: "lookup", Parameters: json.RawMessage(`{"type":"object","properties":{"child":{"$ref":"#/$defs/child"}},"required":["child"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}
	strict, err := goai.ResolveJSONSchemaStrictSampling(tool, true)
	if err != nil {
		t.Fatal(err)
	}
	if strict != nil {
		t.Fatalf("preferred unsupported strict schema should fall back, got %#v", strict)
	}
	tool.ConstrainedSampling.Strict = "require"
	_, err = goai.ResolveJSONSchemaStrictSampling(tool, true)
	if err == nil || !strings.Contains(err.Error(), "$ref schemas are unsupported") {
		t.Fatalf("expected unsupported required schema error, got %v", err)
	}
}
