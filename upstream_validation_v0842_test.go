package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Port of upstream packages/ai/test/validation.test.ts v0.84.2:
// validateToolArguments / treats null as omission for optional non-nullable properties.
func TestUpstreamValidationTreatsNullAsOmissionForOptionalNonNullableProperties(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"number"},"nullable":{"anyOf":[{"type":"string"},{"type":"null"}]},"metadata":{"type":"object","properties":{"enabled":{"type":"boolean"}}}},"required":["path","metadata"]}`)
	tool := goai.Tool{Name: "echo", Description: "Echo tool", Parameters: params}
	toolCall := goai.ToolCall{Type: "toolCall", ID: "tool-1", Name: "echo", Arguments: map[string]interface{}{"path": "file.txt", "offset": nil, "nullable": nil, "metadata": map[string]interface{}{"enabled": nil}}}

	got, err := goai.ValidateToolArguments(&tool, toolCall)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["offset"]; ok {
		t.Fatalf("optional non-nullable offset should be omitted, got %#v", got)
	}
	if got["nullable"] != nil {
		t.Fatalf("nullable optional should preserve null, got %#v", got["nullable"])
	}
	metadata, ok := got["metadata"].(map[string]interface{})
	if !ok || len(metadata) != 0 {
		t.Fatalf("nested optional non-nullable should be omitted, got %#v", got["metadata"])
	}
}

// Port of upstream packages/ai/test/validation.test.ts v0.84.2:
// validateToolArguments / preserves optional nulls whose referenced schema is nullable.
func TestUpstreamValidationPreservesOptionalNullsWithRefSchema(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"value":{"$ref":"#/$defs/value"}},"$defs":{"value":{"anyOf":[{"type":"number"},{"type":"null"}]}}}`)
	tool := goai.Tool{Name: "echo", Description: "Echo tool", Parameters: params}
	toolCall := goai.ToolCall{Type: "toolCall", ID: "tool-1", Name: "echo", Arguments: map[string]interface{}{"value": nil}}

	got, err := goai.ValidateToolArguments(&tool, toolCall)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["value"]; !ok || got["value"] != nil {
		t.Fatalf("referenced nullable null should be preserved, got %#v", got)
	}
}
