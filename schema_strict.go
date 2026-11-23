package goai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
)

type unsupportedStrictJSONSchemaError struct{ message string }

func (e unsupportedStrictJSONSchemaError) Error() string { return e.message }

var unsupportedStrictSchemaKeys = []string{
	"$ref",
	"$defs",
	"definitions",
	"allOf",
	"oneOf",
	"patternProperties",
	"dependentSchemas",
	"dependencies",
	"unevaluatedProperties",
	"propertyNames",
	"contains",
	"prefixItems",
	"not",
	"if",
	"then",
	"else",
}

// StrictSchemaUnsupportedKeyword reports whether a provider rejects the given
// schema keyword/value in strict constrained-sampling mode.
type StrictSchemaUnsupportedKeyword func(key string, value interface{}) bool

// MakeStrictJSONSchema converts a JSON Schema tool parameter object into the
// strict subset accepted by constrained provider tool sampling. It mirrors
// pi-ai's strict conversion: every object property becomes required, optional
// non-nullable properties are widened with a null arm, and objects disallow
// additional properties. Unsupported schema constructs return an error.
func MakeStrictJSONSchema(parameters json.RawMessage) (json.RawMessage, error) {
	return MakeStrictJSONSchemaWithUnsupportedKeyword(parameters, nil)
}

// MakeStrictJSONSchemaWithUnsupportedKeyword is MakeStrictJSONSchema with an
// optional provider-specific strict-keyword rejection hook.
func MakeStrictJSONSchemaWithUnsupportedKeyword(parameters json.RawMessage, isUnsupported StrictSchemaUnsupportedKeyword) (json.RawMessage, error) {
	if len(parameters) == 0 {
		return nil, unsupportedStrictJSONSchemaError{"root schema must have type object"}
	}
	decoder := json.NewDecoder(bytes.NewReader(parameters))
	value, order, err := decodeStrictSchemaJSON(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("unexpected JSON after root schema")
	}
	schema, ok := value.(map[string]interface{})
	if !ok {
		return nil, unsupportedStrictJSONSchemaError{"root schema must have type object"}
	}
	if err := makeJSONSchemaNodeStrict(schema, order, isUnsupported); err != nil {
		return nil, err
	}
	if schema["type"] != "object" {
		return nil, unsupportedStrictJSONSchemaError{"root schema must have type object"}
	}
	out, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveJSONSchemaStrictSampling resolves whether a tool should be emitted as
// strict JSON-schema constrained sampling. It falls back to non-strict for
// unsupported preferred schemas and errors for strict:"require" schemas.
func ResolveJSONSchemaStrictSampling(tool Tool, supportsStrictMode bool) (*bool, error) {
	return ResolveJSONSchemaStrictSamplingWithUnsupportedKeyword(tool, supportsStrictMode, nil)
}

// ResolveJSONSchemaStrictSamplingWithUnsupportedKeyword resolves strict
// JSON-schema constrained sampling with a provider-specific keyword hook.
func ResolveJSONSchemaStrictSamplingWithUnsupportedKeyword(tool Tool, supportsStrictMode bool, isUnsupported StrictSchemaUnsupportedKeyword) (*bool, error) {
	cfg := tool.ConstrainedSampling
	if cfg == nil || cfg.Type != "json_schema" {
		return nil, nil
	}
	if supportsStrictMode {
		if _, err := MakeStrictJSONSchemaWithUnsupportedKeyword(tool.Parameters, isUnsupported); err == nil {
			v := true
			return &v, nil
		} else {
			var unsupported unsupportedStrictJSONSchemaError
			if _, ok := err.(unsupportedStrictJSONSchemaError); !ok {
				return nil, err
			}
			unsupported = err.(unsupportedStrictJSONSchemaError)
			if cfg.Strict != "require" {
				return nil, nil
			}
			//lint:ignore ST1005 Match the pinned pi diagnostic punctuation.
			return nil, fmt.Errorf("Tool %q requires JSON-schema constrained sampling, but %s.", tool.Name, unsupported.message)
		}
	}
	if cfg.Strict == "require" {
		//lint:ignore ST1005 Match the pinned pi diagnostic punctuation.
		return nil, fmt.Errorf("Tool %q requires JSON-schema constrained sampling, but strict tools are unsupported.", tool.Name)
	}
	return nil, nil
}

// JSONSchemaToolParameters returns the strict provider schema when strict is
// true, otherwise the original tool parameters.
func JSONSchemaToolParameters(tool Tool, strict bool) (json.RawMessage, error) {
	if !strict {
		return tool.Parameters, nil
	}
	return MakeStrictJSONSchema(tool.Parameters)
}

func JSONSchemaToolParametersWithUnsupportedKeyword(tool Tool, strict bool, isUnsupported StrictSchemaUnsupportedKeyword) (json.RawMessage, error) {
	if !strict {
		return tool.Parameters, nil
	}
	return MakeStrictJSONSchemaWithUnsupportedKeyword(tool.Parameters, isUnsupported)
}

// strictSchemaOrder retains JSON object order without changing the map/array
// values passed to provider-specific unsupported-keyword callbacks.
type strictSchemaOrder struct {
	keys     []string
	fields   map[string]*strictSchemaOrder
	elements []*strictSchemaOrder
}

func decodeStrictSchemaJSON(decoder *json.Decoder) (any, *strictSchemaOrder, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	switch token {
	case json.Delim('{'):
		values := map[string]any{}
		order := &strictSchemaOrder{fields: map[string]*strictSchemaOrder{}}
		for decoder.More() {
			name, err := decoder.Token()
			if err != nil {
				return nil, nil, err
			}
			key := name.(string) // For an object, Decoder.Token requires a string key.
			value, child, err := decodeStrictSchemaJSON(decoder)
			if err != nil {
				return nil, nil, err
			}
			if _, exists := values[key]; !exists {
				order.keys = append(order.keys, key)
			}
			values[key], order.fields[key] = value, child
		}
		_, err = decoder.Token()
		return values, order, err
	case json.Delim('['):
		values := []any{}
		order := &strictSchemaOrder{}
		for decoder.More() {
			value, child, err := decodeStrictSchemaJSON(decoder)
			if err != nil {
				return nil, nil, err
			}
			values = append(values, value)
			order.elements = append(order.elements, child)
		}
		_, err = decoder.Token()
		return values, order, err
	default:
		return token, nil, nil
	}
}

func strictJSPropertyNames(keys []string) []string {
	// Object.keys puts canonical uint32 array indices first in numeric order;
	// all other keys retain their declaration order.
	index := func(key string) uint64 {
		const notIndex = 1<<32 - 1
		if key == "" || (key[0] == '0' && len(key) != 1) || key[0] < '0' || key[0] > '9' {
			return notIndex
		}
		n, err := strconv.ParseUint(key, 10, 32)
		if err != nil {
			return notIndex
		}
		return n
	}
	names := append([]string{}, keys...)
	sort.SliceStable(names, func(i, j int) bool { return index(names[i]) < index(names[j]) })
	return names
}

func makeJSONSchemaNodeStrict(schema map[string]interface{}, order *strictSchemaOrder, isUnsupported StrictSchemaUnsupportedKeyword) error {
	if schema == nil {
		return unsupportedStrictJSONSchemaError{"boolean schemas are unsupported"}
	}
	for _, key := range unsupportedStrictSchemaKeys {
		if _, ok := schema[key]; ok {
			return unsupportedStrictJSONSchemaError{fmt.Sprintf("%s schemas are unsupported", key)}
		}
	}
	if isUnsupported != nil {
		keys := make([]string, 0, len(schema))
		for key := range schema {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if isUnsupported(key, schema[key]) {
				return unsupportedStrictJSONSchemaError{fmt.Sprintf("%s: %s is unsupported", key, jsonString(schema[key]))}
			}
		}
	}

	// Bare enums and unions are valid, but a const without a type is not.
	_, hasType := schema["type"]
	_, hasUnion := schema["anyOf"]
	_, hasEnum := schema["enum"]
	if !hasType && !hasUnion && !hasEnum {
		return unsupportedStrictJSONSchemaError{"schemas without a type are unsupported"}
	}

	if rawAnyOf, ok := schema["anyOf"]; ok {
		variants, ok := rawAnyOf.([]interface{})
		if !ok || len(variants) == 0 {
			return unsupportedStrictJSONSchemaError{"anyOf must contain at least one schema"}
		}
		for i, rawVariant := range variants {
			variant, ok := rawVariant.(map[string]interface{})
			if !ok {
				return unsupportedStrictJSONSchemaError{"boolean schemas are unsupported"}
			}
			if isStructuredSchema(variant) {
				return unsupportedStrictJSONSchemaError{"object and array unions are unsupported"}
			}
			if err := makeJSONSchemaNodeStrict(variant, order.fields["anyOf"].elements[i], isUnsupported); err != nil {
				return err
			}
		}
	}

	if rawItems, ok := schema["items"]; ok {
		switch items := rawItems.(type) {
		case []interface{}:
			return unsupportedStrictJSONSchemaError{"tuple schemas are unsupported"}
		case map[string]interface{}:
			if err := makeJSONSchemaNodeStrict(items, order.fields["items"], isUnsupported); err != nil {
				return err
			}
		default:
			return unsupportedStrictJSONSchemaError{"boolean schemas are unsupported"}
		}
	}

	isObject := schema["type"] == "object"
	if _, hasProperties := schema["properties"]; hasProperties && !isObject {
		return unsupportedStrictJSONSchemaError{"properties require type object"}
	}
	if !isObject {
		return nil
	}
	if additional, ok := schema["additionalProperties"]; ok && additional != false {
		return unsupportedStrictJSONSchemaError{"schema-valued or true additionalProperties is unsupported"}
	}
	properties := map[string]interface{}{}
	if rawProperties, ok := schema["properties"]; ok {
		var ok bool
		properties, ok = rawProperties.(map[string]interface{})
		if !ok {
			return unsupportedStrictJSONSchemaError{"object properties must be a schema map"}
		}
	}
	requiredSet := map[string]bool{}
	if rawRequired, ok := schema["required"]; ok {
		required, ok := stringArray(rawRequired)
		if !ok {
			return unsupportedStrictJSONSchemaError{"object required must be a string array"}
		}
		for _, name := range required {
			requiredSet[name] = true
		}
	}

	propertyNames := []string{}
	propertyOrder := order.fields["properties"]
	if propertyOrder != nil {
		propertyNames = strictJSPropertyNames(propertyOrder.keys)
	}
	for name := range requiredSet {
		if _, ok := properties[name]; !ok {
			return unsupportedStrictJSONSchemaError{"required contains an unknown property"}
		}
	}
	for _, name := range propertyNames {
		property, ok := properties[name].(map[string]interface{})
		if !ok {
			return unsupportedStrictJSONSchemaError{"boolean schemas are unsupported"}
		}
		if err := makeJSONSchemaNodeStrict(property, propertyOrder.fields[name], isUnsupported); err != nil {
			return err
		}
		if !requiredSet[name] && !schemaAllowsNull(property) {
			properties[name] = map[string]interface{}{"anyOf": []interface{}{property, map[string]interface{}{"type": "null"}}}
		}
	}
	schema["required"] = propertyNames
	schema["additionalProperties"] = false
	return nil
}

func jsonString(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

func isStructuredSchema(schema map[string]interface{}) bool {
	if schema == nil {
		return false
	}
	for _, typ := range schemaTypes(schema["type"]) {
		if typ == "object" || typ == "array" {
			return true
		}
	}
	_, hasProperties := schema["properties"]
	_, hasItems := schema["items"]
	return hasProperties || hasItems
}

func schemaAllowsNull(schema map[string]interface{}) bool {
	if schema == nil {
		return false
	}
	for _, typ := range schemaTypes(schema["type"]) {
		if typ == "null" {
			return true
		}
	}
	if v, ok := schema["const"]; ok && v == nil {
		return true
	}
	if enum, ok := schema["enum"].([]interface{}); ok {
		for _, v := range enum {
			if v == nil {
				return true
			}
		}
	}
	if variants, ok := schemaArray(schema["anyOf"]); ok {
		for _, variant := range variants {
			if schemaAllowsNull(variant) {
				return true
			}
		}
	}
	return false
}

func schemaArray(raw interface{}) ([]map[string]interface{}, bool) {
	items, ok := raw.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		schema, ok := item.(map[string]interface{})
		if !ok {
			return nil, false
		}
		out = append(out, schema)
	}
	return out, true
}

func stringArray(raw interface{}) ([]string, bool) {
	items, ok := raw.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, name)
	}
	return out, true
}
