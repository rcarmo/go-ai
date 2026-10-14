package durable

import (
	"sort"
	"strconv"
	"strings"
)

// Declaration reference resolution never fetches assets or changes validation.
// A bounded expansion handles local JSON pointers; cycles remain unknown.
const DefaultCodemodeSchemaMaxChars = 16000

func codemodeSchemaType(schema JSON, depth int) string {
	return codemodeSchemaTypeBudget(schema, depth, DefaultCodemodeSchemaMaxChars)
}
func codemodeSchemaTypeBudget(schema JSON, depth, maxChars int) string {
	if maxChars < 1 || maxChars > DefaultCodemodeSchemaMaxChars {
		return "unknown"
	}
	// Bound literals/keys as well as reference expansion before rendering.
	inputNodes, inputBytes := 4096, 65536
	if !codemodeSchemaInputBudget(schema, 0, &inputNodes, &inputBytes) {
		return "unknown"
	}
	budget := 2048
	expanded, ok := expandCodemodeSchema(schema, schema, map[string]bool{}, depth, &budget)
	if !ok {
		return "unknown"
	}
	rendered := codemodeSchemaValue(expanded, depth)
	if codemodeTextUnits(rendered) > maxChars {
		rendered = codemodeSchemaValueMode(expanded, depth, false)
		if codemodeTextUnits(rendered) > maxChars {
			return "unknown"
		}
	}
	return rendered
}
func codemodeSchemaMap(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case JSON:
		return map[string]any(v), true
	case map[string]any:
		return v, true
	}
	return nil, false
}
func codemodePointer(root JSON, ref string) (any, bool) {
	if ref == "#" {
		return root, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	var value any = root
	for _, part := range strings.Split(ref[2:], "/") {
		// Reject malformed pointer escapes instead of inventing a target.
		for i := 0; i < len(part); i++ {
			if part[i] == '~' {
				if i+1 >= len(part) || (part[i+1] != '0' && part[i+1] != '1') {
					return nil, false
				}
				i++
			}
		}
		key := strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		if object, ok := codemodeSchemaMap(value); ok {
			var found bool
			value, found = object[key]
			if !found {
				return nil, false
			}
			continue
		}
		if array, ok := value.([]any); ok {
			index, err := strconv.Atoi(key)
			if err != nil || index < 0 || index >= len(array) || strconv.Itoa(index) != key {
				return nil, false
			}
			value = array[index]
			continue
		}
		return nil, false
	}
	return value, true
}
func expandCodemodeSchema(value any, root JSON, active map[string]bool, depth int, budget *int) (any, bool) {
	*budget--
	if *budget < 0 {
		return nil, false
	}
	if depth > 16 {
		return true, true
	}
	object, ok := codemodeSchemaMap(value)
	if !ok {
		return value, true
	}
	if ref, ok := object["$ref"].(string); ok {
		if active[ref] {
			return true, true
		}
		target, found := codemodePointer(root, ref)
		if !found {
			return true, true
		}
		active[ref] = true
		expanded, ok := expandCodemodeSchema(target, root, active, depth+1, budget)
		delete(active, ref)
		return expanded, ok
	}
	out := JSON{}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		child := object[key]
		switch key {
		case "$defs", "definitions":
			continue // lookup uses root; definitions are not rendered
		case "properties":
			props, ok := codemodeSchemaMap(child)
			if !ok {
				out[key] = child
				continue
			}
			copy := map[string]any{}
			names := make([]string, 0, len(props))
			for name := range props {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				property := props[name]
				expanded, ok := expandCodemodeSchema(property, root, active, depth+1, budget)
				if !ok {
					return nil, false
				}
				copy[name] = expanded
			}
			out[key] = copy
		case "items", "additionalProperties":
			if array, ok := child.([]any); ok {
				items := []any{}
				for _, item := range array {
					expanded, ok := expandCodemodeSchema(item, root, active, depth+1, budget)
					if !ok {
						return nil, false
					}
					items = append(items, expanded)
				}
				out[key] = items
			} else {
				expanded, ok := expandCodemodeSchema(child, root, active, depth+1, budget)
				if !ok {
					return nil, false
				}
				out[key] = expanded
			}
		case "anyOf", "oneOf", "allOf", "prefixItems":
			array, ok := child.([]any)
			if !ok {
				out[key] = child
				continue
			}
			items := []any{}
			for _, item := range array {
				expanded, ok := expandCodemodeSchema(item, root, active, depth+1, budget)
				if !ok {
					return nil, false
				}
				items = append(items, expanded)
			}
			out[key] = items
		default:
			out[key] = child
		}
	}
	return out, true
}
