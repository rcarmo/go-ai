package durable

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const codemodeSupportedFields = "`max_output_tokens` and `timeout_ms`"

func parseCodemodeOptions(raw string) (map[string]json.RawMessage, error) {
	if raw == "" {
		return nil, codemodeSourceError("@options must be a JSON object with supported fields " + codemodeSupportedFields)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, codemodeSourceError(fmt.Sprintf("@options must be valid JSON with supported fields %s: %v", codemodeSupportedFields, err))
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, codemodeSourceError(fmt.Sprintf("@options must be valid JSON with supported fields %s: trailing value", codemodeSupportedFields))
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, codemodeSourceError("@options must be a JSON object with supported fields " + codemodeSupportedFields)
	}
	out := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, codemodeSourceError(fmt.Sprintf("@options must be valid JSON with supported fields %s: %v", codemodeSupportedFields, err))
	}
	return out, nil
}
