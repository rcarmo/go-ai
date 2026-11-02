package goai

// Null membership drives coerce.ts's optional-null pass. String, numeric,
// object and array keywords (including formats) cannot reject a null without
// a type assertion. Keep this separate from strict-provider schema rewriting:
// schemaAllowsNull there answers a different, syntactic question.
// Other value domains and dynamic-reference scope remain separately reviewed.
func (v *toolSchemaValidator) schemaAcceptsNull(schema map[string]interface{}) bool {
	if ref, ok := schema["$ref"].(string); ok && !v.checkReference(nil, ref, schema) {
		return false
	}
	if types := schemaTypes(schema["type"]); len(types) > 0 && !anyMatchesJSONSchemaType(nil, types) {
		return false
	}
	if constant, exists := schema["const"]; exists && constant != nil {
		return false
	}
	if values, ok := schema["enum"].([]interface{}); ok {
		found := false
		for _, value := range values {
			if value == nil {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := schema[keyword].([]interface{}); ok {
			matches := 0
			for _, branch := range branches {
				if v.acceptsRaw(nil, branch) {
					matches++
				}
			}
			if (keyword == "allOf" && matches != len(branches)) || (keyword == "anyOf" && matches == 0) || (keyword == "oneOf" && matches != 1) {
				return false
			}
		}
	}
	if negated, exists := schema["not"]; exists && v.acceptsRaw(nil, negated) {
		return false
	}
	if condition, exists := schema["if"]; exists {
		keyword := "else"
		if v.acceptsRaw(nil, condition) {
			keyword = "then"
		}
		if branch, exists := schema[keyword]; exists && !v.acceptsRaw(nil, branch) {
			return false
		}
	}
	return true
}
