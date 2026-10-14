package durable

import "encoding/json"

func codemodeConstraintSchemas(property map[string]any) []map[string]any {
	schemas := []map[string]any{property}
	variants, ok := property["anyOf"].([]any)
	if !ok {
		variants, _ = property["oneOf"].([]any)
	}
	var branch map[string]any
	count := 0
	for _, variant := range variants {
		object, ok := codemodeSchemaMap(variant)
		if ok && object["type"] == "null" {
			continue
		}
		count++
		branch = object
	}
	if count == 1 && branch != nil {
		schemas = append(schemas, branch)
	}
	out := make([]map[string]any, 0, len(schemas))
	for _, schema := range schemas {
		copy := map[string]any{}
		for key, value := range schema {
			copy[key] = value
		}
		for _, pair := range [][2]string{{"minimum", "exclusiveMinimum"}, {"maximum", "exclusiveMaximum"}} {
			if copy[pair[1]] == true {
				value := copy[pair[0]]
				delete(copy, pair[0])
				copy[pair[1]] = value
			}
		}
		out = append(out, copy)
	}
	return out
}
func codemodeConstraintTags(property map[string]any) []string {
	schemas := codemodeConstraintSchemas(property)
	tags := []string{}
	for _, schema := range schemas {
		integer := schema["type"] == "integer"
		if kinds, ok := schema["type"].([]any); ok {
			for _, kind := range kinds {
				if kind == "integer" {
					integer = true
				}
			}
		}
		if integer {
			tags = append(tags, "@integer")
			break
		}
	}
	for _, key := range []string{"minimum", "exclusiveMinimum", "maximum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "format", "minItems", "maxItems", "minProperties", "maxProperties"} {
		lower := key == "minimum" || key == "exclusiveMinimum" || key == "minLength" || key == "minItems" || key == "minProperties"
		upper := key == "maximum" || key == "exclusiveMaximum" || key == "maxLength" || key == "maxItems" || key == "maxProperties"
		var selected any
		seen := map[string]bool{}
		for _, schema := range schemas {
			value, exists := schema[key]
			if !exists {
				continue
			}
			if lower || upper {
				number, ok := exactNumber(value)
				if !ok {
					continue
				}
				if selected == nil {
					selected = value
				} else {
					prior, _ := exactNumber(selected)
					comparison := number.Cmp(prior)
					if lower && comparison > 0 || upper && comparison < 0 {
						selected = value
					}
				}
				continue
			}
			text, ok := value.(string)
			if !ok {
				if _, ok := exactNumber(value); !ok {
					continue
				}
				raw, err := json.Marshal(value)
				if err != nil {
					continue
				}
				text = string(raw)
			}
			if !seen[text] {
				tags = append(tags, "@"+key+" "+text)
				seen[text] = true
			}
		}
		if selected != nil {
			raw, err := json.Marshal(selected)
			if err == nil {
				tags = append(tags, "@"+key+" "+string(raw))
			}
		}
	}
	for _, schema := range schemas {
		if schema["uniqueItems"] == true {
			tags = append(tags, "@uniqueItems")
			break
		}
	}
	return tags
}
