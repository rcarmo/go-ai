package durable

import (
	"encoding/json"
	"io"
	"strings"
)

func parseCodemodeOptions(raw string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, reject("invalid codemode options JSON")
	}
	out := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, reject("invalid codemode options JSON")
		}
		key, ok := token.(string)
		if !ok {
			return nil, reject("invalid codemode option key")
		}
		if _, exists := out[key]; exists {
			return nil, reject("duplicate codemode option")
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, reject("invalid codemode option value")
		}
		out[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, reject("invalid codemode options JSON")
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, reject("trailing codemode options JSON")
	}
	return out, nil
}
