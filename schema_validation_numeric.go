package goai

import (
	"encoding/json"
	"math"
	"math/big"
	"strings"
)

// Numeric keywords constrain numbers even without a type declaration. Keep
// these checks out of conversion: a converted value can fail this branch's
// bounds but satisfy another branch of the enclosing union.
func schemaNumericBoundsReason(value interface{}, schema map[string]interface{}) string {
	if !matchesJSONSchemaType(value, "number") {
		return ""
	}
	for _, keyword := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
		bound, ok := schema[keyword].(float64)
		if !ok {
			continue // Upstream ignores boolean draft-04 exclusive bounds.
		}
		comparison, finite := compareSchemaNumber(value, bound)
		if !finite {
			return "expected number"
		}
		operator := ""
		switch {
		case keyword == "minimum" && comparison < 0:
			operator = ">="
		case keyword == "maximum" && comparison > 0:
			operator = "<="
		case keyword == "exclusiveMinimum" && comparison <= 0:
			operator = ">"
		case keyword == "exclusiveMaximum" && comparison >= 0:
			operator = "<"
		}
		if operator != "" {
			return "expected " + operator + " " + SafeJSONStringify(bound)
		}
	}
	return ""
}

func compareSchemaNumber(value interface{}, bound float64) (int, bool) {
	if math.IsNaN(bound) || math.IsInf(bound, 0) {
		return 0, false
	}
	switch number := value.(type) {
	case int:
		return compareSchemaInt64(int64(number), bound), true
	case int64:
		return compareSchemaInt64(number, bound), true
	case float64:
		return compareSchemaFloat(number, bound)
	case json.Number:
		if integer, err := number.Int64(); err == nil {
			return compareSchemaInt64(integer, bound), true
		}
		parsed, err := number.Float64()
		if err != nil {
			return 0, false
		}
		comparison, finite := compareSchemaFloat(parsed, bound)
		if !finite || comparison != 0 || strings.ContainsAny(string(number), ".eE") {
			return comparison, finite
		}
		// Only canonical integer lexemes outside int64 need the larger path.
		// Parsing float64 first bounds the integer's magnitude; no arbitrary
		// exponent can trigger an unbounded big-number allocation here.
		integer, ok := new(big.Int).SetString(string(number), 10)
		if !ok {
			return comparison, finite
		}
		limit, _ := new(big.Float).SetFloat64(bound).Int(nil)
		return integer.Cmp(limit), true
	default:
		return 0, false
	}
}

func compareSchemaFloat(value, bound float64) (int, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	if value < bound {
		return -1, true
	}
	if value > bound {
		return 1, true
	}
	return 0, true
}

func compareSchemaInt64(value int64, bound float64) int {
	comparison, _ := compareSchemaFloat(float64(value), bound)
	if comparison != 0 {
		return comparison
	}
	// A rounded tie needs an exact comparison. 2^63 cannot convert to int64;
	// every int64 value that rounds to that boundary is strictly smaller.
	if bound == 9223372036854775808.0 {
		return -1
	}
	limit := int64(bound)
	if value < limit {
		return -1
	}
	if value > limit {
		return 1
	}
	return 0
}
