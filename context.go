// Context utilities — overflow detection, tool validation, token management.
package goai

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
)

// --- Context overflow detection ---

var overflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),
	regexp.MustCompile(`(?i)request_too_large`),
	regexp.MustCompile(`(?i)input is too long for requested model`),
	regexp.MustCompile(`(?i)exceeds the context window`),
	regexp.MustCompile(`(?i)exceeds (?:the )?(?:model'?s )?maximum context length(?: of [\d,]+ tokens?|\s*\([\d,]+\))`), // OpenAI/LiteLLM specific
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),
	regexp.MustCompile(`(?i)reduce the length of the messages`),
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),
	regexp.MustCompile(`(?i)exceeds (?:the )?maximum allowed input length of [\d,]+ tokens?`),                // OpenRouter/Poolside
	regexp.MustCompile(`(?i)input \(\d+ tokens\) is longer than the model'?s context length \(\d+ tokens\)`), // Together AI
	regexp.MustCompile(`(?i)exceeds the limit of \d+`),
	regexp.MustCompile(`(?i)exceeds the available context size`),
	regexp.MustCompile(`(?i)prompt has [\d,]+ tokens?, but the configured context size is [\d,]+ tokens?`),
	regexp.MustCompile(`(?i)greater than the context length`),
	regexp.MustCompile(`(?i)context window exceeds limit`),
	regexp.MustCompile(`(?i)exceeded model token limit`),
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`),
	regexp.MustCompile(`(?i)model_context_window_exceeded`),
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`),
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),
	regexp.MustCompile(`(?i)too many tokens`),
	regexp.MustCompile(`(?i)token limit exceeded`),
	regexp.MustCompile(`(?i)^(?:HTTP\s*)?4(?:00|13|29)\s*:?(?:\s*status code)?\s*\(no body\)`),
}

var nonOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(Throttling error|Service unavailable):`),
	regexp.MustCompile(`(?i)rate limit`),
	regexp.MustCompile(`(?i)too many requests`),
}

// IsContextOverflow checks if a message represents a context window overflow.
func IsContextOverflow(msg *Message, contextWindow int) bool {
	if msg == nil {
		return false
	}
	if msg.StopReason == StopReasonError {
		texts := overflowCandidateTexts(msg)
		for _, text := range texts {
			for _, p := range nonOverflowPatterns {
				if p.MatchString(text) {
					return false
				}
			}
		}
		for _, text := range texts {
			for _, p := range overflowPatterns {
				if p.MatchString(text) {
					return true
				}
			}
		}
	}
	if contextWindow > 0 && msg.StopReason == StopReasonStop && msg.Usage != nil {
		inputTokens := msg.Usage.Input + msg.Usage.CacheRead
		if inputTokens > contextWindow {
			return true
		}
	}
	// Case 3: Length-stop overflow (Xiaomi MiMo style) — server truncates oversized
	// input to fit the context window, leaving no room for output. Returns
	// stopReason "length" with output=0 and input+cacheRead filling the window.
	if contextWindow > 0 && msg.StopReason == StopReasonLength && msg.Usage != nil && msg.Usage.Output == 0 {
		inputTokens := msg.Usage.Input + msg.Usage.CacheRead
		if float64(inputTokens) >= float64(contextWindow)*0.99 {
			return true
		}
	}
	return false
}

func overflowCandidateTexts(msg *Message) []string {
	texts := make([]string, 0, 1+len(msg.Diagnostics)*2)
	if msg.ErrorMessage != "" {
		texts = append(texts, msg.ErrorMessage)
	}
	for _, d := range msg.Diagnostics {
		if d.Error.Message != "" {
			texts = append(texts, d.Error.Message)
		}
		if d.Error.Code != nil {
			texts = append(texts, fmt.Sprint(d.Error.Code))
		}
	}
	return texts
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
	out := make(map[string]interface{}, len(tc.Arguments))
	for k, v := range tc.Arguments {
		out[k] = v
	}
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for name, val := range out {
			propSchema, ok := properties[name].(map[string]interface{})
			if !ok {
				continue
			}
			coerced, err := validateAndCoerceType(name, val, propSchema)
			if err != nil {
				return nil, fmt.Errorf("validation failed for tool %q: %w", tool.Name, err)
			}
			out[name] = coerced
		}
	}
	return out, nil
}

func validateAndCoerceType(name string, value interface{}, schema map[string]interface{}) (interface{}, error) {
	return coerceWithJSONSchema(name, value, schema)
}

func coerceWithJSONSchema(name string, value interface{}, schema map[string]interface{}) (interface{}, error) {
	if schemas, ok := schemaList(schema["allOf"]); ok {
		next := value
		for _, nested := range schemas {
			coerced, err := coerceWithJSONSchema(name, next, nested)
			if err != nil {
				return nil, err
			}
			next = coerced
		}
		value = next
	}
	if schemas, ok := schemaList(schema["anyOf"]); ok {
		return coerceWithUnionSchema(name, value, schemas)
	}
	if schemas, ok := schemaList(schema["oneOf"]); ok {
		return coerceWithUnionSchema(name, value, schemas)
	}

	types := schemaTypes(schema["type"])
	if len(types) == 0 {
		return value, nil
	}
	matchesUnionMember := len(types) > 1 && anyMatchesJSONSchemaType(value, types)
	if !matchesUnionMember {
		var lastErr error
		for _, expectedType := range types {
			if matchesJSONSchemaType(value, expectedType) && schemaConstraintsMatch(value, expectedType, schema) {
				return value, nil
			}
			coerced, err := coerceJSONSchemaType(value, expectedType)
			if err == nil && schemaConstraintsMatch(coerced, expectedType, schema) {
				return coerced, nil
			}
			if err != nil {
				lastErr = err
			} else if expectedType == "string" {
				lastErr = fmt.Errorf("field %q: value %q not in enum", name, coerced)
			}
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("field %q: expected %v, got %T", name, types, value)
		}
		return nil, lastErr
	}
	return value, nil
}

func coerceWithUnionSchema(name string, value interface{}, schemas []map[string]interface{}) (interface{}, error) {
	for _, schema := range schemas {
		if schemaAcceptsValue(value, schema) {
			return value, nil
		}
	}
	var lastErr error
	for _, schema := range schemas {
		coerced, err := coerceWithJSONSchema(name, cloneValidationValue(value), schema)
		if err == nil && schemaAcceptsValue(coerced, schema) {
			return coerced, nil
		}
		if err != nil {
			lastErr = err
		}
	}
	if lastErr == nil {
		return value, nil
	}
	return nil, lastErr
}

func schemaList(raw interface{}) ([]map[string]interface{}, bool) {
	items, ok := raw.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if schema, ok := item.(map[string]interface{}); ok {
			out = append(out, schema)
		}
	}
	return out, len(out) > 0
}

func schemaAcceptsValue(value interface{}, schema map[string]interface{}) bool {
	types := schemaTypes(schema["type"])
	if len(types) == 0 {
		return true
	}
	for _, expectedType := range types {
		if matchesJSONSchemaType(value, expectedType) && schemaConstraintsMatch(value, expectedType, schema) {
			return true
		}
	}
	return false
}

func schemaConstraintsMatch(value interface{}, expectedType string, schema map[string]interface{}) bool {
	if expectedType != "string" {
		return true
	}
	enum, ok := schema["enum"].([]interface{})
	if !ok {
		return true
	}
	s, ok := value.(string)
	if !ok {
		return false
	}
	for _, e := range enum {
		if e == s {
			return true
		}
	}
	return false
}

func anyMatchesJSONSchemaType(value interface{}, types []string) bool {
	for _, expectedType := range types {
		if matchesJSONSchemaType(value, expectedType) {
			return true
		}
	}
	return false
}

func cloneValidationValue(value interface{}) interface{} {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return value
	}
	return out
}

func schemaTypes(raw interface{}) []string {
	switch t := raw.(type) {
	case string:
		return []string{t}
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func matchesJSONSchemaType(value interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case float64, int, int64, json.Number:
			return true
		default:
			return false
		}
	case "integer":
		switch v := value.(type) {
		case int, int64:
			return true
		case float64:
			return v == math.Trunc(v)
		case json.Number:
			f, err := v.Float64()
			return err == nil && f == math.Trunc(f)
		default:
			return false
		}
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	default:
		return false
	}
}

func coerceJSONSchemaType(value interface{}, expectedType string) (interface{}, error) {
	switch expectedType {
	case "string":
		switch v := value.(type) {
		case string:
			return v, nil
		case nil:
			return "", nil
		case bool:
			if v {
				return "true", nil
			}
			return "false", nil
		default:
			return nil, fmt.Errorf("expected string, got %T", value)
		}
	case "number":
		return coerceNumber(value, false)
	case "integer":
		return coerceNumber(value, true)
	case "boolean":
		switch v := value.(type) {
		case bool:
			return v, nil
		case string:
			if v == "true" {
				return true, nil
			}
			if v == "false" {
				return false, nil
			}
		case float64:
			if v == 1 {
				return true, nil
			}
			if v == 0 {
				return false, nil
			}
		case int:
			if v == 1 {
				return true, nil
			}
			if v == 0 {
				return false, nil
			}
		case int64:
			if v == 1 {
				return true, nil
			}
			if v == 0 {
				return false, nil
			}
		}
		return nil, fmt.Errorf("expected boolean, got %T", value)
	case "null":
		switch v := value.(type) {
		case nil:
			return nil, nil
		case string:
			if v == "" {
				return nil, nil
			}
		case float64:
			if v == 0 {
				return nil, nil
			}
		case int:
			if v == 0 {
				return nil, nil
			}
		case int64:
			if v == 0 {
				return nil, nil
			}
		case bool:
			if !v {
				return nil, nil
			}
		}
		return nil, fmt.Errorf("expected null, got %T", value)
	case "array":
		if _, ok := value.([]interface{}); !ok {
			return nil, fmt.Errorf("expected array, got %T", value)
		}
	case "object":
		if _, ok := value.(map[string]interface{}); !ok {
			return nil, fmt.Errorf("expected object, got %T", value)
		}
	}
	return value, nil
}

func coerceNumber(value interface{}, integer bool) (interface{}, error) {
	var f float64
	switch v := value.(type) {
	case float64:
		f = v
	case int:
		f = float64(v)
	case int64:
		f = float64(v)
	case json.Number:
		parsed, err := v.Float64()
		if err != nil {
			return nil, err
		}
		f = parsed
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, err
		}
		f = parsed
	case bool:
		if v {
			f = 1
		} else {
			f = 0
		}
	case nil:
		f = 0
	default:
		return nil, fmt.Errorf("expected number, got %T", value)
	}
	if integer && f != math.Trunc(f) {
		return nil, fmt.Errorf("expected integer, got %v", f)
	}
	if integer {
		return int(f), nil
	}
	return f, nil
}
