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
	regexp.MustCompile(`(?i)prompt (?:is )?too long`),
	regexp.MustCompile(`(?i)prompt exceeds max length`),
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
	regexp.MustCompile(`(?i)^(?:HTTP\s*)?4(?:00|13)\s*:?(?:\s*status code)?\s*\(no body\)`),
	regexp.MustCompile(`(?i)range of input length should be`),
	// HTTP 413 means request-size overflow regardless of provider or body text
	// (e.g. GitHub Copilot "failed to parse request"). Match the formatted
	// provider-error prefix or a bare status, not a status mentioned in the body.
	regexp.MustCompile(`(?i)^(?:[\w -]+ API error \(413\):|413(?:\s|:|$))`),
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
	if tc.ArgumentsError != "" {
		return nil, fmt.Errorf("invalid tool call arguments for tool %q: %s", tc.Name, tc.ArgumentsError)
	}
	if len(tool.Parameters) == 0 {
		return tc.Arguments, nil
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(tool.Parameters, &schema); err != nil {
		return tc.Arguments, nil
	}
	v := newToolSchemaValidator(schema, tool.Parameters)
	out := v.normalizeOptionalNulls(tc.Arguments, schema).(map[string]interface{})
	if out == nil {
		out = make(map[string]interface{})
	}
	if required, ok := schema["required"].([]interface{}); ok {
		for _, r := range required {
			name, ok := r.(string)
			if !ok {
				continue
			}
			if _, exists := out[name]; !exists {
				return nil, fmt.Errorf("validation failed for tool %q: missing required field %q", tool.Name, name)
			}
		}
	}
	coerced, err := v.coerceWithJSONSchema(out, schema)
	if err != nil {
		if issue, ok := err.(*toolSchemaIssue); ok {
			received, marshalErr := json.MarshalIndent(tc.Arguments, "", "  ")
			if marshalErr != nil {
				received = []byte("<non-JSON arguments>")
			}
			//lint:ignore ST1005 The upstream outcome protocol recognises this exact capitalised prefix.
			return nil, fmt.Errorf("Validation failed for tool %q:\n  - %s\n\nReceived arguments:\n%s", tool.Name, issue, received)
		}
		return nil, fmt.Errorf("validation failed for tool %q: %w", tool.Name, err)
	}
	result, ok := coerced.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("validation failed for tool %q: arguments must be an object", tool.Name)
	}
	return result, nil
}

// Return owned containers along traversed schema paths: removing a nested null
// must not mutate the caller's arguments. Pattern/additional keys themselves
// stay present; only their nested declared optional properties can be removed.
func (validator *toolSchemaValidator) normalizeOptionalNulls(value interface{}, schema map[string]interface{}) interface{} {
	switch v := value.(type) {
	case []interface{}:
		out := append([]interface{}(nil), v...)
		if v != nil && out == nil {
			out = []interface{}{}
		}
		if itemSchemas, ok := schema["items"].([]interface{}); ok {
			for i := range out {
				if i >= len(itemSchemas) {
					break
				}
				if itemSchema, ok := itemSchemas[i].(map[string]interface{}); ok {
					out[i] = validator.normalizeOptionalNulls(out[i], itemSchema)
				}
			}
		} else if itemSchema, ok := schema["items"].(map[string]interface{}); ok {
			for i := range out {
				out[i] = validator.normalizeOptionalNulls(out[i], itemSchema)
			}
		}
		return out
	case map[string]interface{}:
		if v == nil {
			return v
		}
		out := make(map[string]interface{}, len(v))
		for key, current := range v {
			out[key] = current
		}
		properties, _ := schema["properties"].(map[string]interface{})
		required := map[string]bool{}
		if rawRequired, ok := schema["required"].([]interface{}); ok {
			for _, r := range rawRequired {
				if name, ok := r.(string); ok {
					required[name] = true
				}
			}
		}
		for key, rawProperty := range properties {
			propertySchema, objectSchema := rawProperty.(map[string]interface{})
			current, exists := v[key]
			if !exists {
				continue
			}
			if current == nil && !required[key] && !hasStringRef(propertySchema) && !validator.acceptsRaw(nil, rawProperty) {
				delete(out, key)
				continue
			}
			if objectSchema {
				out[key] = validator.normalizeOptionalNulls(current, propertySchema)
			}
		}
		patterns, _ := schema["patternProperties"].(map[string]interface{})
		// Go's RE2 syntax covers these upstream probes; ECMAScript-only regex
		// constructs and full pattern validation still need separate review.
		matchedKeys := make(map[string]bool)
		unsupportedPattern := false
		for pattern, rawPattern := range patterns {
			patternSchema, _ := rawPattern.(map[string]interface{})
			re, err := regexp.Compile(pattern)
			if err != nil {
				unsupportedPattern = true
				continue
			}
			for key, current := range out {
				if re.MatchString(key) {
					matchedKeys[key] = true
					out[key] = validator.normalizeOptionalNulls(current, patternSchema)
				}
			}
		}
		if additional, ok := schema["additionalProperties"].(map[string]interface{}); ok && !unsupportedPattern {
			for key, current := range out {
				if _, declared := properties[key]; declared || matchedKeys[key] {
					continue
				}
				out[key] = validator.normalizeOptionalNulls(current, additional)
			}
		}
		return out
	default:
		return value
	}
}

func hasStringRef(schema map[string]interface{}) bool {
	_, ok := schema["$ref"].(string)
	return ok
}

func (v *toolSchemaValidator) coerceWithJSONSchema(value interface{}, schema map[string]interface{}) (interface{}, error) {
	converted := v.convertValidationValue(value, schema)
	if !v.schemaAcceptsValue(converted, schema) {
		return nil, v.issue(converted, schema, "", make(map[toolSchemaMemoKey]bool))
	}
	return converted, nil
}

// Conversion is separate from validation: a failed branch conversion can still
// produce a value accepted by another branch after sibling members convert.
// Each candidate owns changed containers; failed attempts cannot mutate input.
func (v *toolSchemaValidator) convertValidationValue(value interface{}, schema map[string]interface{}) interface{} {
	if v.work != nil {
		v.work.convertCalls++
	}
	if ref, ok := schema["$ref"].(string); ok {
		return v.convertReference(value, ref, schema)
	}
	if branches, ok := schemaList(schema["anyOf"]); ok {
		return v.convertValidationUnion(value, schema, branches)
	}
	if branches, ok := schemaList(schema["oneOf"]); ok {
		return v.convertValidationUnion(value, schema, branches)
	}
	if branches, ok := schemaList(schema["allOf"]); ok {
		for _, branch := range branches {
			value = v.convertValidationValue(value, branch)
		}
		return v.convertValidationMembers(value, schema)
	}
	types := schemaTypes(schema["type"])
	if len(types) == 1 && types[0] == "array" {
		if _, array := value.([]interface{}); !array {
			_, tuple := validationTupleItems(schema)
			if tuple {
				return value
			}
			value = []interface{}{value}
		}
	} else if len(types) > 0 && !(len(types) > 1 && anyMatchesJSONSchemaType(value, types)) {
		for _, expectedType := range types {
			if matchesJSONSchemaType(value, expectedType) && schemaConstraintsMatch(value, expectedType, schema) {
				break
			}
			var converted interface{}
			var err error
			if kind, scalar := schema["type"].(string); scalar {
				converted, err = convertJSONSchemaPrimitive(value, kind)
			} else {
				converted, err = coerceJSONSchemaType(value, expectedType)
			}
			if err == nil && schemaConstraintsMatch(converted, expectedType, schema) {
				value = converted
				break
			}
		}
	}
	if len(types) == 1 {
		switch types[0] {
		case "string", "number", "integer", "boolean", "null":
			return value
		case "object":
			if _, object := value.(map[string]interface{}); !object {
				return value
			}
		}
	}
	return v.convertValidationMembers(value, schema)
}

func (v *toolSchemaValidator) convertValidationUnion(value interface{}, schema map[string]interface{}, branches []map[string]interface{}) interface{} {
	for _, branch := range branches {
		if v.schemaAcceptsValue(value, branch) {
			return v.convertValidationMembers(value, schema)
		}
	}
	for _, branch := range branches {
		candidate := v.convertValidationMembers(v.convertValidationValue(value, branch), schema)
		// Whole-node validation includes sibling properties and exact oneOf
		// cardinality. A branch-only match cannot establish a valid candidate.
		if v.schemaAcceptsValue(candidate, schema) {
			return candidate
		}
	}
	return v.convertValidationMembers(value, schema)
}

func (v *toolSchemaValidator) convertValidationMembers(value interface{}, schema map[string]interface{}) interface{} {
	switch value := value.(type) {
	case map[string]interface{}:
		if value == nil {
			return value
		}
		members := v.membersFor(schema)
		if !members.hasSchemas() {
			return value
		}
		out := make(map[string]interface{}, len(value))
		for key, current := range value {
			members.each(key, func(raw interface{}) bool {
				if property, ok := raw.(map[string]interface{}); ok {
					current = v.convertValidationValue(current, property)
				}
				return true
			})
			out[key] = current
		}
		return out
	case []interface{}:
		if value == nil {
			return value
		}
		out := make([]interface{}, len(value))
		for i, current := range value {
			if item := validationItemSchema(schema, i); item != nil {
				current = v.convertValidationValue(current, item)
			}
			out[i] = current
		}
		return out
	default:
		return value
	}
}

func validationTupleItems(schema map[string]interface{}) ([]interface{}, bool) {
	if items, ok := schema["items"].([]interface{}); ok {
		return items, true
	}
	items, ok := schema["prefixItems"].([]interface{})
	return items, ok
}

func validationItemSchema(schema map[string]interface{}, index int) map[string]interface{} {
	if items, tuple := validationTupleItems(schema); tuple {
		if index < len(items) {
			item, _ := items[index].(map[string]interface{})
			return item
		}
		// Legacy tuple items have no homogeneous rest schema.
		if _, legacy := schema["items"].([]interface{}); legacy {
			return nil
		}
	}
	item, _ := schema["items"].(map[string]interface{})
	return item
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

// Membership checks never coerce: union branch selection must not count a
// numeric string as an integer or an absent required property as a match.
func (v *toolSchemaValidator) schemaAcceptsValue(value interface{}, schema map[string]interface{}) bool {
	if value == nil {
		return v.schemaAcceptsNull(schema)
	}
	if ref, ok := schema["$ref"].(string); ok && !v.checkReference(value, ref, schema) {
		return false
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := schemaList(schema[keyword]); ok {
			matches := 0
			for _, branch := range branches {
				if v.schemaAcceptsValue(value, branch) {
					matches++
				}
			}
			if (keyword == "allOf" && matches != len(branches)) ||
				(keyword == "anyOf" && matches == 0) || (keyword == "oneOf" && matches != 1) {
				return false
			}
		}
	}
	if _, isString := value.(string); isString && !schemaConstraintsMatch(value, "string", schema) {
		return false
	}
	types := schemaTypes(schema["type"])
	if len(types) > 0 {
		matched := false
		for _, expectedType := range types {
			if matchesJSONSchemaType(value, expectedType) && schemaConstraintsMatch(value, expectedType, schema) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if schemaNumericBoundsReason(value, schema) != "" || v.stringFormatReason(value, schema) != "" {
		return false
	}
	switch value := value.(type) {
	case map[string]interface{}:
		if required, ok := schema["required"].([]interface{}); ok {
			for _, raw := range required {
				if key, ok := raw.(string); ok {
					if _, present := value[key]; !present {
						return false
					}
				}
			}
		}
		members := v.membersFor(schema)
		for key, current := range value {
			if !members.each(key, func(raw interface{}) bool { return v.acceptsRaw(current, raw) }) {
				return false
			}
		}
	case []interface{}:
		for i, current := range value {
			if item := validationItemSchema(schema, i); item != nil && !v.schemaAcceptsValue(current, item) {
				return false
			}
		}
	}
	return true
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
			return !math.IsInf(v, 0) && v == math.Trunc(v)
		case json.Number:
			_, integer, err := schemaTruncateDecimal(v)
			return err == nil && integer
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
		case float64:
			return schemaFloatString(v), nil
		case int:
			return strconv.FormatInt(int64(v), 10), nil
		case int64:
			return strconv.FormatInt(v, 10), nil
		case json.Number:
			return schemaDecimalString(v)
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
		if integer {
			_, exact, err := schemaTruncateDecimal(v)
			if err != nil || !exact {
				return nil, fmt.Errorf("expected integer")
			}
			return v, nil
		}
		parsed, err := v.Float64()
		if err != nil {
			return nil, err
		}
		f = parsed
	case string:
		parsed, ok := schemaParseNumber(v, false)
		if !ok {
			return nil, fmt.Errorf("expected number")
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
	if integer && (math.IsInf(f, 0) || f != math.Trunc(f)) {
		return nil, fmt.Errorf("expected integer, got %v", f)
	}
	if integer {
		return schemaIntegerValue(f), nil
	}
	return f, nil
}
