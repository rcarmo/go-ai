package goai

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"unicode/utf8"
)

// Constraints participate in membership as well as diagnostics: union selection
// must not regard a branch with failed bounds as a match.
func (v *toolSchemaValidator) constraintIssue(value interface{}, schema map[string]interface{}, path string, active map[toolSchemaMemoKey]bool) *toolSchemaIssue {
	fail := func(reason string) *toolSchemaIssue { return &toolSchemaIssue{path, reason} }
	if text, ok := value.(string); ok {
		for _, bound := range []struct {
			key, description string
			below            bool
		}{{"minLength", "at least", true}, {"maxLength", "at most", false}} {
			if n, ok := schema[bound.key].(float64); ok && (bound.below && float64(utf8.RuneCountInString(text)) < n || !bound.below && float64(utf8.RuneCountInString(text)) > n) {
				return fail("expected " + bound.description + " " + schemaFloatString(n) + " characters")
			}
		}
	}
	if divisor, ok := schema["multipleOf"].(float64); ok && matchesJSONSchemaType(value, "number") && !schemaNumberMultiple(value, divisor) {
		return fail("expected a multiple of " + schemaFloatString(divisor))
	}
	if object, ok := value.(map[string]interface{}); ok {
		if names, exists := schema["propertyNames"]; exists {
			for _, key := range sortedSchemaKeys(object) {
				if !v.acceptsRaw(key, names) {
					return &toolSchemaIssue{schemaIssuePath(path, key), "invalid property name"}
				}
			}
		}
		for _, b := range []struct {
			key, description string
			below            bool
		}{{"minProperties", "at least", true}, {"maxProperties", "at most", false}} {
			if n, ok := schema[b.key].(float64); ok && (b.below && float64(len(object)) < n || !b.below && float64(len(object)) > n) {
				return fail("expected " + b.description + " " + schemaFloatString(n) + " properties")
			}
		}
		// Legacy dependencies override dependentRequired for a duplicate key.
		dependencies := map[string]interface{}{}
		for _, keyword := range []string{"dependentRequired", "dependencies"} {
			if entries, ok := schema[keyword].(map[string]interface{}); ok {
				for key, entry := range entries {
					dependencies[key] = entry
				}
			}
		}
		for _, key := range sortedSchemaKeys(dependencies) {
			if _, present := object[key]; !present {
				continue
			}
			raw := dependencies[key]
			if required, ok := raw.([]interface{}); ok {
				for _, item := range required {
					if name, ok := item.(string); ok {
						if _, present := object[name]; !present {
							return &toolSchemaIssue{schemaIssuePath(path, name), "required when " + key + " is present"}
						}
					}
				}
			} else if !v.acceptsRaw(value, raw) {
				return v.rawIssue(value, raw, path, active)
			}
		}
		if entries, ok := schema["dependentSchemas"].(map[string]interface{}); ok {
			for _, key := range sortedSchemaKeys(entries) {
				if _, present := object[key]; present && !v.acceptsRaw(value, entries[key]) {
					return v.rawIssue(value, entries[key], path, active)
				}
			}
		}
	}
	if array, ok := value.([]interface{}); ok {
		for _, b := range []struct {
			key, description string
			below            bool
		}{{"minItems", "at least", true}, {"maxItems", "at most", false}} {
			if n, ok := schema[b.key].(float64); ok && (b.below && float64(len(array)) < n || !b.below && float64(len(array)) > n) {
				return fail("expected " + b.description + " " + schemaFloatString(n) + " items")
			}
		}
		if unique, _ := schema["uniqueItems"].(bool); unique && !schemaUniqueItems(array) {
			return fail("expected unique items")
		}
		if contains, present := schema["contains"]; present {
			min, max := 1.0, math.Inf(1)
			if n, ok := schema["minContains"].(float64); ok {
				min = n
			}
			if n, ok := schema["maxContains"].(float64); ok {
				max = n
			}
			matches := 0
			for _, item := range array {
				if v.acceptsRaw(item, contains) {
					matches++
				}
			}
			if float64(matches) < min || float64(matches) > max {
				noun := "items"
				if min == 1 {
					noun = "item"
				}
				if math.IsInf(max, 1) {
					return fail("expected at least " + schemaFloatString(min) + " matching " + noun)
				}
				if min == 0 {
					noun := "items"
					if max == 1 {
						noun = "item"
					}
					return fail("expected at most " + schemaFloatString(max) + " matching " + noun)
				}
				return fail("expected between " + schemaFloatString(min) + " and " + schemaFloatString(max) + " matching items")
			}
		}
	}
	if negated, present := schema["not"]; present && v.acceptsRaw(value, negated) {
		return fail("must not match the schema in not")
	}
	if condition, present := schema["if"]; present {
		key := "else"
		if v.acceptsRaw(value, condition) {
			key = "then"
		}
		if branch, present := schema[key]; present && !v.acceptsRaw(value, branch) {
			return v.rawIssue(value, branch, path, active)
		}
	}
	if raw, present := schema["unevaluatedProperties"]; present {
		if object, ok := value.(map[string]interface{}); ok {
			evaluated := v.evaluatedProperties(value, schema, make(map[uintptr]bool))
			for _, key := range sortedSchemaKeys(object) {
				if evaluated[key] {
					continue
				}
				if !v.acceptsRaw(object[key], raw) {
					if no, ok := raw.(bool); ok && !no {
						return &toolSchemaIssue{schemaIssuePath(path, key), "unknown field"}
					}
					return v.rawIssue(object[key], raw, schemaIssuePath(path, key), active)
				}
			}
		}
	}
	return nil
}

func (v *toolSchemaValidator) rawIssue(value, raw interface{}, path string, active map[toolSchemaMemoKey]bool) *toolSchemaIssue {
	if no, ok := raw.(bool); ok && !no {
		return &toolSchemaIssue{path, "no value is allowed here"}
	}
	if child, ok := raw.(map[string]interface{}); ok {
		return v.issue(value, child, path, active)
	}
	return &toolSchemaIssue{path, "value does not match schema"}
}

func validationItemRaw(schema map[string]interface{}, index int) interface{} {
	if prefix, tuple := validationTupleItems(schema); tuple {
		if index < len(prefix) {
			return prefix[index]
		}
		if _, legacy := schema["items"].([]interface{}); legacy {
			return schema["additionalItems"]
		}
	}
	return schema["items"]
}

func schemaNumberMultiple(value interface{}, divisor float64) bool {
	if divisor == 0 || math.IsNaN(divisor) || math.IsInf(divisor, 0) {
		return false
	}
	text, ok := schemaLiteralNumber(value)
	if !ok {
		return false
	}
	// Bound exponent expansion for native exact decimal extensions too.
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsInf(n, 0) || n == 0 && text != "0" {
		return false
	}
	q := n / divisor
	_, sourceNumber := value.(float64)
	if sourceNumber {
		if math.IsInf(q, 0) {
			return math.Mod(n, divisor) == 0
		}
		if q == math.Trunc(q) {
			return true
		}
	}
	a, ok := new(big.Rat).SetString(text)
	if !ok {
		return false
	}
	b, ok := new(big.Rat).SetString(schemaFloatString(divisor))
	if !ok || b.Sign() == 0 {
		return false
	}
	return new(big.Rat).Quo(a, b).IsInt()
}

// Canonical exact numeric spellings make 1 and 1.0 equal without conflating
// numbers and strings. Encoding/json sorts object keys. Hash-map membership
// avoids quadratic comparison work for large arrays.
func schemaUniqueItems(items []interface{}) bool {
	seen := make(map[string]bool, len(items))
	active := make(map[toolSchemaValueKey]bool)
	nodes := 0
	var canonical func(interface{}, int) (interface{}, error)
	canonical = func(value interface{}, depth int) (interface{}, error) {
		nodes++
		if depth > 256 || nodes > 1<<20 {
			return nil, fmt.Errorf("unique item work limit")
		}
		if key, ok := toolSchemaKey(nil, value); ok {
			if key.value.ptr != 0 {
				if active[key.value] {
					return nil, fmt.Errorf("cyclic unique item")
				}
				active[key.value] = true
				defer delete(active, key.value)
			}
		}
		if number, ok := schemaLiteralNumber(value); ok {
			return json.Number(number), nil
		}
		switch value := value.(type) {
		case map[string]interface{}:
			out := make(map[string]interface{}, len(value))
			for key, item := range value {
				x, err := canonical(item, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = x
			}
			return out, nil
		case []interface{}:
			out := make([]interface{}, len(value))
			for i, item := range value {
				x, err := canonical(item, depth+1)
				if err != nil {
					return nil, err
				}
				out[i] = x
			}
			return out, nil
		case nil, string, bool:
			return value, nil
		default:
			return nil, fmt.Errorf("non-JSON unique item")
		}
	}
	for _, item := range items {
		nodes++
		if nodes > 1<<20 {
			return false
		}
		var key string
		// Scalars need no detached tree or JSON encoder. Preserve exact numeric
		// equality and keep the number/string domains distinct.
		switch value := item.(type) {
		case int:
			key = "n:" + strconv.Itoa(value)
		case int64:
			key = "n:" + strconv.FormatInt(value, 10)
		case float64:
			if math.IsInf(value, 0) || math.IsNaN(value) {
				return false
			}
			key = "n:" + schemaFloatString(value)
		case json.Number:
			number, ok := schemaLiteralNumber(value)
			if !ok {
				return false
			}
			key = "n:" + number
		case string:
			key = "s:" + value
		case bool:
			key = "b:" + strconv.FormatBool(value)
		case nil:
			key = "null"
		default:
			value, err := canonical(item, 0)
			if err != nil {
				return false
			}
			data, err := json.Marshal(value)
			if err != nil {
				return false
			}
			key = "j:" + string(data)
		}
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}

// Collect the property annotations of successful in-place applicators. A local
// visited set cuts recursive references without confusing independent values.
func (v *toolSchemaValidator) evaluatedProperties(value interface{}, schema map[string]interface{}, seen map[uintptr]bool) map[string]bool {
	result := make(map[string]bool)
	id := reflect.ValueOf(schema).Pointer()
	if seen[id] {
		return result
	}
	seen[id] = true
	object, ok := value.(map[string]interface{})
	if !ok {
		return result
	}
	members := v.membersFor(schema)
	for key := range object {
		if _, declared := members.properties[key]; declared {
			result[key] = true
		}
		for _, pattern := range members.patterns {
			if v.matchSchemaPattern(pattern.source, key).matched {
				result[key] = true
			}
		}
		if members.additional != nil {
			result[key] = true
		}
	}
	add := func(raw interface{}) {
		if child, ok := raw.(map[string]interface{}); ok {
			for key := range v.evaluatedProperties(value, child, seen) {
				result[key] = true
			}
		}
	}
	if ref, ok := schema["$ref"].(string); ok {
		add(v.resolvePureDataReference(ref, schema))
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := schema[keyword].([]interface{}); ok {
			for _, branch := range branches {
				if v.acceptsRaw(value, branch) {
					add(branch)
				}
			}
		}
	}
	if condition, present := schema["if"]; present {
		if v.acceptsRaw(value, condition) {
			add(condition)
			add(schema["then"])
		} else {
			add(schema["else"])
		}
	}
	for _, keyword := range []string{"dependentSchemas", "dependencies"} {
		if entries, ok := schema[keyword].(map[string]interface{}); ok {
			for key, entry := range entries {
				if _, present := object[key]; present {
					add(entry)
				}
			}
		}
	}
	return result
}
