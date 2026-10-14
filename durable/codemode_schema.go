package durable

import (
	"encoding/json"
	"sort"
	"strings"
)

// Render the already-validated native tool schema subset. Unsupported schema
// syntax stays unknown; declarations never grant execution or validation rights.
func codemodeSchemaType(schema JSON, depth int) string {
	if depth > 16 {
		return "unknown"
	}
	if value, ok := schema["const"]; ok {
		data, err := json.Marshal(value)
		if err != nil {
			return "unknown"
		}
		return string(data)
	}
	for _, keyword := range []string{"anyOf", "oneOf", "allOf"} {
		if branches, ok := schema[keyword].([]any); ok && len(branches) > 0 {
			parts := []string{}
			for _, branch := range branches {
				parts = append(parts, codemodeSchemaValue(branch, depth+1))
			}
			separator := " | "
			if keyword == "allOf" {
				separator = " & "
				for i, part := range parts {
					if strings.Contains(part, " | ") {
						parts[i] = "(" + part + ")"
					}
				}
			}
			return strings.Join(parts, separator)
		}
	}
	if kinds, ok := schema["type"].([]any); ok {
		parts := []string{}
		for _, kind := range kinds {
			copy := JSON{}
			for key, value := range schema {
				copy[key] = value
			}
			copy["type"] = kind
			parts = append(parts, codemodeSchemaType(copy, depth+1))
		}
		return strings.Join(parts, " | ")
	}
	if values, ok := schema["enum"].([]any); ok && len(values) > 0 {
		parts := []string{}
		for _, value := range values {
			data, e := json.Marshal(value)
			if e != nil {
				return "unknown"
			}
			parts = append(parts, string(data))
		}
		return strings.Join(parts, " | ")
	}
	kind := schema["type"]
	if kind == nil {
		if _, ok := schema["properties"]; ok {
			kind = "object"
		} else if _, ok := schema["additionalProperties"]; ok {
			kind = "object"
		} else if _, ok := schema["items"]; ok {
			kind = "array"
		} else if _, ok := schema["prefixItems"]; ok {
			kind = "array"
		}
	}
	switch kind {
	case "string":
		return "string"
	case "integer", "number":
		return "number"
	case "boolean":
		return "boolean"
	case "null":
		return "null"
	case "array":
		if item, exists := schema["items"]; exists {
			if _, tuple := item.([]any); !tuple {
				return "Array<" + codemodeSchemaValue(item, depth+1) + ">"
			}
		}
		tuple, ok := schema["prefixItems"].([]any)
		if !ok {
			tuple, _ = schema["items"].([]any)
		}
		if len(tuple) == 0 {
			return "unknown[]"
		}
		parts := make([]string, 0, len(tuple))
		for _, item := range tuple {
			parts = append(parts, codemodeSchemaValue(item, depth+1))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case "object":
		properties, _ := schema["properties"].(map[string]any)
		required := map[string]bool{}
		if list, ok := schema["required"].([]any); ok {
			for _, name := range list {
				if text, ok := name.(string); ok {
					required[text] = true
				}
			}
		}
		keys := []string{}
		for key := range properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := []string{}
		for _, key := range keys {
			child := properties[key]
			name := key
			if CodemodeIdentifier(key) != key {
				data, _ := json.Marshal(key)
				name = string(data)
			}
			optional := "?"
			if required[key] {
				optional = ""
			}
			parts = append(parts, name+optional+": "+codemodeSchemaValue(child, depth+1)+";")
		}
		switch extra := schema["additionalProperties"].(type) {
		case map[string]any:
			parts = append(parts, "[key: string]: "+codemodeSchemaValue(extra, depth+1)+";")
		case JSON:
			parts = append(parts, "[key: string]: "+codemodeSchemaValue(extra, depth+1)+";")
		case bool:
			if extra {
				parts = append(parts, "[key: string]: unknown;")
			}
		default:
			parts = append(parts, "[key: string]: unknown;")
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, " ") + " }"
	default:
		return "unknown"
	}
}

func codemodeSchemaValue(value any, depth int) string {
	if depth > 16 {
		return "unknown"
	}
	switch schema := value.(type) {
	case bool:
		if schema {
			return "unknown"
		}
		return "never"
	case map[string]any:
		return codemodeSchemaType(JSON(schema), depth)
	case JSON:
		return codemodeSchemaType(schema, depth)
	}
	return "unknown"
}
