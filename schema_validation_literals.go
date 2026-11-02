package goai

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
)

// JSON value equality, not Go representation equality: numeric values share a
// domain, booleans do not; member order is irrelevant, array order is not.
// Exact native integers/decimals are never rounded through float64.
func schemaLiteralEqual(a, b interface{}) bool {
	type pair struct {
		a, b  uintptr
		array bool
	}
	var active map[pair]bool
	var equal func(interface{}, interface{}) bool
	equal = func(a, b interface{}) bool {
		if a == nil || b == nil {
			return a == nil && b == nil
		}
		switch a := a.(type) {
		case string:
			b, ok := b.(string)
			return ok && a == b
		case bool:
			b, ok := b.(bool)
			return ok && a == b
		case map[string]interface{}:
			b, ok := b.(map[string]interface{})
			if !ok || len(a) != len(b) {
				return false
			}
			key := pair{reflect.ValueOf(a).Pointer(), reflect.ValueOf(b).Pointer(), false}
			if key.a == key.b {
				return true
			}
			if active[key] {
				return false
			}
			if active == nil {
				active = make(map[pair]bool)
			}
			active[key] = true
			defer delete(active, key)
			for k, v := range a {
				other, exists := b[k]
				if !exists || !equal(v, other) {
					return false
				}
			}
			return true
		case []interface{}:
			b, ok := b.([]interface{})
			if !ok || len(a) != len(b) {
				return false
			}
			if len(a) == 0 {
				return true
			}
			key := pair{reflect.ValueOf(a).Pointer(), reflect.ValueOf(b).Pointer(), true}
			if key.a == key.b {
				return true
			}
			if active[key] {
				return false
			}
			if active == nil {
				active = make(map[pair]bool)
			}
			active[key] = true
			defer delete(active, key)
			for i, v := range a {
				if !equal(v, b[i]) {
					return false
				}
			}
			return true
		default:
			x, xok := schemaLiteralNumber(a)
			y, yok := schemaLiteralNumber(b)
			return xok && yok && x == y
		}
	}
	return equal(a, b)
}

func schemaLiteralNumber(value interface{}) (string, bool) {
	var number json.Number
	switch value := value.(type) {
	case int:
		number = json.Number(strconv.Itoa(value))
	case int64:
		number = json.Number(strconv.FormatInt(value, 10))
	case float64:
		if math.IsInf(value, 0) || math.IsNaN(value) {
			return "", false
		}
		number = json.Number(schemaFloatString(value))
	case json.Number:
		number = value
	default:
		return "", false
	}
	s, err := schemaDecimalString(number)
	return s, err == nil
}

func schemaLiteralsMatch(value interface{}, schema map[string]interface{}) bool {
	if constant, exists := schema["const"]; exists && !schemaLiteralEqual(value, constant) {
		return false
	}
	if values, ok := schema["enum"].([]interface{}); ok {
		for _, member := range values {
			if schemaLiteralEqual(value, member) {
				return true
			}
		}
		return false
	}
	return true
}

func schemaLiteralTypeName(value interface{}) string {
	switch value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case int, int64, float64, json.Number:
		if matchesJSONSchemaType(value, "integer") {
			return "integer"
		}
		return "number"
	case []interface{}:
		return "array"
	default:
		return "object"
	}
}
