package google

import (
	"bytes"
	"encoding/json"
	"io"
)

// Decode into an owned tree without rounding JSON numeric literals through float64.
func decodeGoogleToolSchema(raw json.RawMessage) (any, bool) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return value, true
}

func rewriteGoogleConstSchema(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	value, ok := decodeGoogleToolSchema(raw)
	if !ok {
		return append(json.RawMessage(nil), raw...)
	}
	out, err := json.Marshal(rewriteGoogleConstSchemaValue(value, false))
	if err != nil {
		return append(json.RawMessage(nil), raw...)
	}
	return out
}

// Only schema-bearing keywords recurse. Defaults, examples, enum members and
// object-valued consts are literal data, even when they resemble schemas.
func rewriteGoogleConstSchemaValue(value any, nameMap bool) any {
	switch v := value.(type) {
	case []any:
		for i, child := range v {
			v[i] = rewriteGoogleConstSchemaValue(child, false)
		}
	case map[string]any:
		for key, child := range v {
			if nameMap {
				v[key] = rewriteGoogleConstSchemaValue(child, false)
				continue
			}
			switch key {
			case "properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies":
				v[key] = rewriteGoogleConstSchemaValue(child, true)
			case "additionalProperties", "unevaluatedProperties", "propertyNames", "items", "prefixItems", "additionalItems", "unevaluatedItems", "contains", "allOf", "anyOf", "oneOf", "not", "if", "then", "else", "contentSchema":
				v[key] = rewriteGoogleConstSchemaValue(child, false)
			}
		}
		if !nameMap {
			if _, exists := v["enum"]; !exists {
				switch literal := v["const"].(type) {
				case string:
					v["enum"] = []any{literal}
					delete(v, "const")
				case json.Number:
					v["enum"] = []any{literal}
					delete(v, "const")
				}
			}
		}
	}
	return value
}
