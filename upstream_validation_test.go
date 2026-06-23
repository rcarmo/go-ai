package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func upstreamCreateToolCallWithPlainSchema(t *testing.T, schema map[string]interface{}, value interface{}) (goai.Tool, goai.ToolCall) {
	t.Helper()
	params := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"value": schema,
		},
		"required": []string{"value"},
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	tool := goai.Tool{Name: "echo", Description: "Echo tool", Parameters: data}
	toolCall := goai.ToolCall{Type: "toolCall", ID: "tool-1", Name: "echo", Arguments: map[string]interface{}{"value": value}}
	return tool, toolCall
}

// Port of upstream packages/ai/test/validation.test.ts:
// validateToolArguments / "still validates when Function constructor is unavailable".
func TestUpstreamValidationStillValidatesWhenFunctionConstructorUnavailable(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"count":{"type":"number"}},"required":["count"]}`)
	tool := goai.Tool{Name: "echo", Description: "Echo tool", Parameters: params}
	toolCall := goai.ToolCall{Type: "toolCall", ID: "tool-1", Name: "echo", Arguments: map[string]interface{}{"count": "42"}}
	got, err := goai.ValidateToolArguments(&tool, toolCall)
	if err != nil {
		t.Fatal(err)
	}
	if got["count"] != float64(42) {
		t.Fatalf("expected coerced count 42, got %#v", got)
	}
}

// Port of upstream packages/ai/test/validation.test.ts:
// validateToolArguments / "coerces serialized plain JSON schemas with AJV-compatible primitive rules".
func TestUpstreamValidationCoercesSerializedPlainJSONSchemas(t *testing.T) {
	tests := []struct {
		schema   map[string]interface{}
		input    interface{}
		expected interface{}
	}{
		{map[string]interface{}{"type": "number"}, "42", float64(42)},
		{map[string]interface{}{"type": "number"}, true, float64(1)},
		{map[string]interface{}{"type": "number"}, nil, float64(0)},
		{map[string]interface{}{"type": "integer"}, "42", 42},
		{map[string]interface{}{"type": "boolean"}, "true", true},
		{map[string]interface{}{"type": "boolean"}, "false", false},
		{map[string]interface{}{"type": "boolean"}, 1, true},
		{map[string]interface{}{"type": "boolean"}, 0, false},
		{map[string]interface{}{"type": "string"}, nil, ""},
		{map[string]interface{}{"type": "string"}, true, "true"},
		{map[string]interface{}{"type": "null"}, "", nil},
		{map[string]interface{}{"type": "null"}, 0, nil},
		{map[string]interface{}{"type": "null"}, false, nil},
		{map[string]interface{}{"type": []interface{}{"number", "string"}}, "1", "1"},
		{map[string]interface{}{"type": []interface{}{"boolean", "number"}}, "1", float64(1)},
	}
	for _, tt := range tests {
		tool, toolCall := upstreamCreateToolCallWithPlainSchema(t, tt.schema, tt.input)
		got, err := goai.ValidateToolArguments(&tool, toolCall)
		if err != nil {
			t.Fatalf("schema=%#v input=%#v: %v", tt.schema, tt.input, err)
		}
		if got["value"] != tt.expected {
			t.Fatalf("schema=%#v input=%#v: expected %#v, got %#v", tt.schema, tt.input, tt.expected, got["value"])
		}
	}
}

// Port of upstream packages/ai/test/validation.test.ts:
// validateToolArguments / "rejects invalid coercions for serialized plain JSON schemas".
func TestUpstreamValidationRejectsInvalidCoercions(t *testing.T) {
	tests := []struct {
		schema map[string]interface{}
		input  interface{}
	}{
		{map[string]interface{}{"type": "boolean"}, "1"},
		{map[string]interface{}{"type": "boolean"}, "0"},
		{map[string]interface{}{"type": "null"}, "null"},
		{map[string]interface{}{"type": "integer"}, "42.1"},
	}
	for _, tt := range tests {
		tool, toolCall := upstreamCreateToolCallWithPlainSchema(t, tt.schema, tt.input)
		_, err := goai.ValidateToolArguments(&tool, toolCall)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "validation failed") {
			t.Fatalf("schema=%#v input=%#v: expected validation failed error, got %v", tt.schema, tt.input, err)
		}
	}
}
