package goai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// MarshalJSON retains the optional errorMessage arm, including an explicit
// empty string, without adding a second wire property for Go presence state.
func (m ClassifierResult) MarshalJSON() ([]byte, error) {
	type plain ClassifierResult
	var errorMessage *string
	if m.ErrorMessagePresent || m.ErrorMessage != "" {
		errorMessage = &m.ErrorMessage
	}
	return json.Marshal(struct {
		plain
		ErrorMessage *string `json:"errorMessage,omitempty"`
	}{plain: plain(m), ErrorMessage: errorMessage})
}

func (m *ClassifierResult) UnmarshalJSON(data []byte) error {
	type plain ClassifierResult
	value := plain{}
	wire := struct {
		*plain
		ErrorMessage json.RawMessage `json:"errorMessage"`
	}{plain: &value}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing result JSON")
	}
	if len(wire.ErrorMessage) != 0 && !bytes.Equal(bytes.TrimSpace(wire.ErrorMessage), []byte("null")) {
		if err := json.Unmarshal(wire.ErrorMessage, &value.ErrorMessage); err != nil {
			return err
		}
		value.ErrorMessagePresent = value.ErrorMessage == ""
	}
	*m = ClassifierResult(value)
	return nil
}
