package durable

import (
	"net/url"
	"sort"
	"strings"
	"unicode/utf8"
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

// ResolveCodemodeSchemaRef resolves local schema pointers without fetching
// resources. It returns the original object or boolean, not a detached copy.
func ResolveCodemodeSchemaRef(ref string, root any) (any, bool) {
	if ref != "#" && !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	value := root
	if ref != "#" {
		for _, part := range strings.Split(ref[2:], "/") {
			if part == "" {
				continue
			}
			decoded, err := url.PathUnescape(part)
			if err != nil || !utf8.ValidString(decoded) {
				return nil, false
			}
			object, ok := codemodeSchemaMap(value)
			if !ok || object == nil {
				return nil, false
			}
			key := strings.ReplaceAll(strings.ReplaceAll(decoded, "~1", "/"), "~0", "~")
			value, ok = object[key]
			if !ok {
				return nil, false
			}
		}
	}
	if _, ok := value.(bool); ok {
		return value, true
	}
	if object, ok := codemodeSchemaMap(value); ok && object != nil {
		return value, true
	}
	return nil, false
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
		target, found := ResolveCodemodeSchemaRef(ref, root)
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
