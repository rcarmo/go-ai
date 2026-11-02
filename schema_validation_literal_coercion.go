package goai

func convertSchemaLiteral(literal, value interface{}) (interface{}, bool) {
	if schemaLiteralEqual(literal, value) {
		return value, false
	}
	kind := ""
	switch literal.(type) {
	case string:
		kind = "string"
	case bool:
		kind = "boolean"
	case float64:
		kind = "number"
	default:
		return value, false
	}
	candidate, err := convertJSONSchemaPrimitive(value, kind)
	if err == nil && schemaLiteralEqual(candidate, literal) {
		return candidate, true
	}
	return value, false
}

// Pass 3 follows the complete TypeBox-style pass, not individual union
// candidates. It deliberately does not follow $ref (coerce.ts at c9f4c7b9).
func (v *toolSchemaValidator) coerceValidationTypes(value interface{}, schema map[string]interface{}) interface{} {
	if branches, ok := schemaList(schema["allOf"]); ok {
		for _, branch := range branches {
			value = v.coerceValidationTypes(value, branch)
		}
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		if branches, ok := schemaList(schema[keyword]); ok {
			matched := false
			for _, branch := range branches {
				if v.schemaAcceptsValue(value, branch) {
					matched = true
					break
				}
			}
			if !matched {
				for _, branch := range branches {
					candidate := v.coerceValidationTypes(value, branch)
					if v.schemaAcceptsValue(candidate, branch) {
						value = candidate
						break
					}
				}
			}
		}
	}
	types := schemaTypes(schema["type"])
	if !(len(types) > 1 && anyMatchesJSONSchemaType(value, types)) {
		for _, kind := range types {
			if matchesJSONSchemaType(value, kind) {
				continue
			}
			candidate, err := coerceJSONSchemaType(value, kind)
			if err == nil {
				value = candidate
				break
			}
		}
	}
	for _, kind := range types {
		switch kind {
		case "object":
			if object, ok := value.(map[string]interface{}); ok && object != nil {
				out := make(map[string]interface{}, len(object))
				members := v.membersFor(schema)
				for key, current := range object {
					members.each(key, func(raw interface{}) bool {
						if child, ok := raw.(map[string]interface{}); ok {
							current = v.coerceValidationTypes(current, child)
						}
						return true
					})
					out[key] = current
				}
				return out
			}
		case "array":
			if array, ok := value.([]interface{}); ok {
				out := append([]interface{}(nil), array...)
				if array != nil && out == nil {
					out = []interface{}{}
				}
				for i, current := range out {
					if child := validationItemSchema(schema, i); child != nil {
						out[i] = v.coerceValidationTypes(current, child)
					}
				}
				return out
			}
		}
	}
	return value
}
