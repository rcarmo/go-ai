// Context utilities — overflow detection, tool validation, token management.
package goai

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// --- Context overflow detection ---

var overflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),
	regexp.MustCompile(`(?i)request_too_large`),
	regexp.MustCompile(`(?i)input is too long for requested model`),
	regexp.MustCompile(`(?i)exceeds the context window`),
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),
	regexp.MustCompile(`(?i)reduce the length of the messages`),
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),
	regexp.MustCompile(`(?i)exceeds the limit of \d+`),
	regexp.MustCompile(`(?i)exceeds the available context size`),
	regexp.MustCompile(`(?i)greater than the context length`),
	regexp.MustCompile(`(?i)context window exceeds limit`),
	regexp.MustCompile(`(?i)exceeded model token limit`),
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),
	regexp.MustCompile(`(?i)model_context_window_exceeded`),
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),
	regexp.MustCompile(`(?i)too many tokens`),
	regexp.MustCompile(`(?i)token limit exceeded`),
	regexp.MustCompile(`(?i)^4(?:00|13)\s*(?:status code)?\s*\(no body\)`),
}

var nonOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(Throttling error|Service unavailable):`),
	regexp.MustCompile(`(?i)rate limit`),
	regexp.MustCompile(`(?i)too many requests`),
}

// IsContextOverflow checks if a message represents a context window overflow.
func IsContextOverflow(msg *Message, contextWindow int) bool {
	if msg.StopReason == StopReasonError && msg.ErrorMessage != "" {
		for _, p := range nonOverflowPatterns {
			if p.MatchString(msg.ErrorMessage) {
				return false
			}
		}
		for _, p := range overflowPatterns {
			if p.MatchString(msg.ErrorMessage) {
				return true
			}
		}
	}
	if contextWindow > 0 && msg.StopReason == StopReasonStop && msg.Usage != nil {
		inputTokens := msg.Usage.Input + msg.Usage.CacheRead
		if inputTokens > contextWindow {
			return true
		}
	}
	return false
}

// --- Tool call validation ---

// ValidateToolCall finds a tool by name and validates the arguments.
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
func ValidateToolArguments(tool *Tool, tc ToolCall) (map[string]interface{}, error) {
	if len(tool.Parameters) == 0 {
		return tc.Arguments, nil
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
		return tc.Arguments, nil
	}
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
