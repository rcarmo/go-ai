package goai

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

//go:embed model_schemas_generated.json
var officialModelSchemas []byte

// ModelSchema returns a detached JSON schema exported by official pi-ai1.2.0.
// These shared fragments exclude the coding-agent's models.json config surface.
func ModelSchema(name string) (json.RawMessage, error) {
	var catalog struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	}
	if err := json.Unmarshal(officialModelSchemas, &catalog); err != nil {
		return nil, err
	}
	schema, ok := catalog.Schemas[name]
	if !ok {
		return nil, fmt.Errorf("unknown model schema %q", name)
	}
	return append(json.RawMessage(nil), schema...), nil
}

// ValidateModelSchema validates strict JSON data against the fixed official
// schema fragments. It never executes caller marshalers or accepts nonfinite
// numbers; unknown schema keywords fail closed instead of silently weakening it.
func ValidateModelSchema(name string, data []byte) error {
	if len(data) > 8<<20 {
		return fmt.Errorf("model schema value too large")
	}
	raw, err := ModelSchema(name)
	if err != nil {
		return err
	}
	var schema map[string]any
	if err = json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var value any
	if err = decoder.Decode(&value); err != nil {
		return err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("invalid trailing model schema data")
	}
	return validateModelSchemaNode(schema, value, "$", 0)
}
func validateModelSchemaNode(s map[string]any, value any, at string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("model schema depth limit at %s", at)
	}
	for key := range s {
		switch key {
		case "type", "enum", "const", "anyOf", "properties", "patternProperties", "additionalProperties", "required", "items", "maxItems", "minLength", "minimum", "exclusiveMinimum", "maximum", "description", "default":
		default:
			return fmt.Errorf("unsupported model schema keyword %s", key)
		}
	}
	if choices, ok := s["anyOf"].([]any); ok {
		for _, choice := range choices {
			if object, ok := choice.(map[string]any); ok && validateModelSchemaNode(object, value, at, depth+1) == nil {
				return nil
			}
		}
		return fmt.Errorf("model schema union mismatch at %s", at)
	}
	if constant, ok := s["const"]; ok && !reflect.DeepEqual(constant, value) {
		return fmt.Errorf("model schema const mismatch at %s", at)
	}
	if choices, ok := s["enum"].([]any); ok {
		found := false
		for _, c := range choices {
			if reflect.DeepEqual(c, value) {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("model schema enum mismatch at %s", at)
		}
	}
	bad := func() error { return fmt.Errorf("model schema type/constraint mismatch at %s", at) }
	switch s["type"] {
	case "null":
		if value != nil {
			return bad()
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad()
		}
	case "string":
		v, ok := value.(string)
		if !ok {
			return bad()
		}
		if min, ok := s["minLength"].(float64); ok && len([]rune(v)) < int(min) {
			return bad()
		}
	case "number", "integer":
		v, ok := value.(json.Number)
		if !ok {
			return bad()
		}
		n, err := v.Float64()
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return bad()
		}
		// Bound rational parsing before allocation: tiny finite JSON exponents
		// must not force arbitrarily large big.Int denominators.
		spelling := string(v)
		if len(spelling) > 4096 {
			return bad()
		}
		if i := strings.IndexAny(spelling, "eE"); i >= 0 {
			exponent, err := strconv.ParseInt(spelling[i+1:], 10, 32)
			if err != nil || exponent < -4096 || exponent > 4096 {
				return bad()
			}
		}
		exact, ok := new(big.Rat).SetString(spelling)
		if !ok {
			return bad()
		}
		if s["type"] == "integer" && !exact.IsInt() {
			return bad()
		}
		compare := func(bound float64) int {
			other, _ := new(big.Rat).SetString(strconv.FormatFloat(bound, 'g', -1, 64))
			return exact.Cmp(other)
		}
		if min, ok := s["minimum"].(float64); ok && compare(min) < 0 {
			return bad()
		}
		if min, ok := s["exclusiveMinimum"].(float64); ok && compare(min) <= 0 {
			return bad()
		}
		if max, ok := s["maximum"].(float64); ok && compare(max) > 0 {
			return bad()
		}
	case "array":
		a, ok := value.([]any)
		if !ok {
			return bad()
		}
		if max, ok := s["maxItems"].(float64); ok && len(a) > int(max) {
			return bad()
		}
		if child, ok := s["items"].(map[string]any); ok {
			for i, v := range a {
				if err := validateModelSchemaNode(child, v, fmt.Sprintf("%s[%d]", at, i), depth+1); err != nil {
					return err
				}
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return bad()
		}
		if required, ok := s["required"].([]any); ok {
			for _, name := range required {
				if _, ok := object[name.(string)]; !ok {
					return fmt.Errorf("required model field %s at %s", name, at)
				}
			}
		}
		properties, _ := s["properties"].(map[string]any)
		patterns, _ := s["patternProperties"].(map[string]any)
		for key, v := range object {
			matched := false
			if child, ok := properties[key].(map[string]any); ok {
				matched = true
				if err := validateModelSchemaNode(child, v, at+"."+key, depth+1); err != nil {
					return err
				}
			}
			for pattern, raw := range patterns {
				re, err := regexp.Compile(pattern)
				if err != nil {
					return err
				}
				if re.MatchString(key) {
					matched = true
					if err := validateModelSchemaNode(raw.(map[string]any), v, at+"."+key, depth+1); err != nil {
						return err
					}
				}
			}
			if !matched {
				switch extra := s["additionalProperties"].(type) {
				case bool:
					if !extra {
						return fmt.Errorf("unknown model field %s at %s", key, at)
					}
				case map[string]any:
					if err := validateModelSchemaNode(extra, v, at+"."+key, depth+1); err != nil {
						return err
					}
				}
			}
		}
	case nil:
	default:
		return fmt.Errorf("unsupported model schema type %v", s["type"])
	}
	return nil
}
