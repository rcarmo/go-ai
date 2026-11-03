package goai

import (
	"bytes"
	"encoding/json"
	"errors"
)

// TelemetryCarrier holds opaque, adapter-owned trace propagation strings.
// Keys and identifiers have no library-defined interpretation. Do not include
// credentials or application content: durable hosts may persist these fields.
type TelemetryCarrier map[string]string

// UnmarshalJSON rejects non-string values, including JSON null. encoding/json's
// ordinary string-map decoder would silently convert a null value to "".
func (c *TelemetryCarrier) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return errors.New("goai: telemetry carrier must be an object of strings")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	owned := make(TelemetryCarrier, len(fields))
	for key, raw := range fields {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || raw[0] != '"' {
			return errors.New("goai: telemetry carrier values must be strings")
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		owned[key] = value
	}
	*c = owned
	return nil
}
