package goai

import (
	"encoding/json"
	"fmt"
	"sort"
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

// MakeStrictJSONSchema converts a JSON Schema tool parameter object into the
// strict subset accepted by constrained provider tool sampling. It mirrors
// pi-ai's strict conversion: every object property becomes required, optional
// non-nullable properties are widened with a null arm, and objects disallow
// additional properties. Unsupported schema constructs return an error.
func MakeStrictJSONSchema(parameters json.RawMessage) (json.RawMessage, error) {
	if len(parameters) == 0 {
		return nil, unsupportedStrictJSONSchemaError{"root schema must have type object"}
	}
	var schema map[string]interface{}
	if err := json.Unmarshal(parameters, &schema); err != nil {
		return nil, err
	}
	if err := makeJSONSchemaNodeStrict(schema); err != nil {
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
	cfg := tool.ConstrainedSampling
	if cfg == nil || cfg.Type != "json_schema" {
		return nil, nil
	}
	if supportsStrictMode {
		if _, err := MakeStrictJSONSchema(tool.Parameters); err == nil {
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
			return nil, fmt.Errorf("tool %q requires JSON-schema constrained sampling, but %s", tool.Name, unsupported.message)
		}
	}
	if cfg.Strict == "require" {
		return nil, fmt.Errorf("tool %q requires JSON-schema constrained sampling, but strict tools are unsupported", tool.Name)
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

func makeJSONSchemaNodeStrict(schema map[string]interface{}) error {
	if schema == nil {
		return unsupportedStrictJSONSchemaError{"boolean schemas are unsupported"}
	}
	for _, key := range unsupportedStrictSchemaKeys {
		if _, ok := schema[key]; ok {
			return unsupportedStrictJSONSchemaError{fmt.Sprintf("%s schemas are unsupported", key)}
		}
	}

	if rawAnyOf, ok := schema["anyOf"]; ok {
		variants, ok := schemaArray(rawAnyOf)
		if !ok || len(variants) == 0 {
			return unsupportedStrictJSONSchemaError{"anyOf must contain at least one schema"}
		}
		for _, variant := range variants {
			if isStructuredSchema(variant) {
				return unsupportedStrictJSONSchemaError{"object and array unions are unsupported"}
			}
			if err := makeJSONSchemaNodeStrict(variant); err != nil {
				return err
			}
		}
	}

	if rawItems, ok := schema["items"]; ok {
		switch items := rawItems.(type) {
		case []interface{}:
			return unsupportedStrictJSONSchemaError{"tuple schemas are unsupported"}
		case map[string]interface{}:
			if err := makeJSONSchemaNodeStrict(items); err != nil {
				return err
			}
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

	propertyNames := make([]string, 0, len(properties))
	for name := range properties {
		propertyNames = append(propertyNames, name)
	}
	sort.Strings(propertyNames)
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
		if err := makeJSONSchemaNodeStrict(property); err != nil {
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
