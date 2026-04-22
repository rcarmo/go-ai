// Tool call argument validation against JSON Schema.
package goai

import (
	"encoding/json"
	"fmt"
)

// ValidateToolCall finds a tool by name and validates the arguments.
// Returns the (potentially coerced) arguments map.
func ValidateToolCall(tools []Tool, tc ToolCall) (map[string]interface{}, error) {
	var tool *Tool
	for i := range tools {
		if tools[i].Name == tc.Name {
			tool = &tools[i]
			break
		}
	}
	if tool == nil {
		return nil, fmt.Errorf("tool %q not found", tc.Name)
	}
	return ValidateToolArguments(tool, tc)
}

// ValidateToolArguments validates tool call arguments against the tool's JSON Schema.
//
// This performs structural validation: checks required fields, types, and enum values.
// For full JSON Schema validation, use a dedicated library like github.com/santhosh-tekuri/jsonschema.
func ValidateToolArguments(tool *Tool, tc ToolCall) (map[string]interface{}, error) {
	if len(tool.Parameters) == 0 {
		return tc.Arguments, nil
	}

	// Parse the schema
	var schema map[string]interface{}
	if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
		return tc.Arguments, nil // can't parse schema, pass through
	}

	// Check required fields
	if required, ok := schema["required"].([]interface{}); ok {
		for _, r := range required {
			name, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := tc.Arguments[name]; !exists {
				return nil, fmt.Errorf("validation failed for tool %q: missing required field %q", tool.Name, name)
			}
		}
	}

	// Check property types
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for name, val := range tc.Arguments {
			propSchema, ok := properties[name].(map[string]interface{})
			if !ok {
				continue
			}
			if err := validateType(name, val, propSchema); err != nil {
				return nil, fmt.Errorf("validation failed for tool %q: %w", tool.Name, err)
			}
		}
	}

	return tc.Arguments, nil
}

func validateType(name string, value interface{}, schema map[string]interface{}) error {
	expectedType, ok := schema["type"].(string)
	if !ok {
		return nil
	}

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field %q: expected string, got %T", name, value)
		}
		// Check enum
		if enum, ok := schema["enum"].([]interface{}); ok {
			s := value.(string)
			found := false
			for _, e := range enum {
				if e == s {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("field %q: value %q not in enum", name, s)
			}
		}
	case "number", "integer":
		switch value.(type) {
		case float64, int, int64, json.Number:
			// ok
		default:
			return fmt.Errorf("field %q: expected number, got %T", name, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("field %q: expected boolean, got %T", name, value)
		}
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return fmt.Errorf("field %q: expected array, got %T", name, value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Errorf("field %q: expected object, got %T", name, value)
		}
	}

	return nil
}
