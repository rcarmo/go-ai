package goai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// MarshalJSON retains the optional errorMessage arm, including an explicit
// empty string, without adding a second wire property for Go presence state.
func (m Message) MarshalJSON() ([]byte, error) {
	type plain Message
	var errorMessage *string
	if m.ErrorMessagePresent || m.ErrorMessage != "" {
		errorMessage = &m.ErrorMessage
	}
	sections, err := marshalMessageSections(m)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		plain
		ErrorMessage *string         `json:"errorMessage,omitempty"`
		Sections     json.RawMessage `json:"sections,omitempty"`
	}{plain: plain(m), ErrorMessage: errorMessage, Sections: sections})
}

func (m *Message) UnmarshalJSON(data []byte) error {
	type plain Message
	value := plain{}
	wire := struct {
		*plain
		ErrorMessage json.RawMessage `json:"errorMessage"`
		Sections     json.RawMessage `json:"sections"`
	}{plain: &value}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("trailing message JSON")
	}
	if len(wire.ErrorMessage) != 0 && !bytes.Equal(bytes.TrimSpace(wire.ErrorMessage), []byte("null")) {
		if err := json.Unmarshal(wire.ErrorMessage, &value.ErrorMessage); err != nil {
			return err
		}
		value.ErrorMessagePresent = value.ErrorMessage == ""
	}
	if len(wire.Sections) != 0 && !bytes.Equal(bytes.TrimSpace(wire.Sections), []byte("null")) {
		if err := json.Unmarshal(wire.Sections, &value.Sections); err != nil {
			return err
		}
		fields := json.NewDecoder(bytes.NewReader(wire.Sections))
		if _, err := fields.Token(); err != nil {
			return err
		}
		for fields.More() {
			key, err := fields.Token()
			if err != nil {
				return err
			}
			value.SectionOrder = append(value.SectionOrder, key.(string))
			var ignored json.RawMessage
			if err := fields.Decode(&ignored); err != nil {
				return err
			}
		}
	}
	*m = Message(value)
	return nil
}

func marshalMessageSections(message Message) (json.RawMessage, error) {
	if len(message.Sections) == 0 {
		return nil, nil
	}
	var out bytes.Buffer
	out.WriteByte('{')
	for index, key := range transcriptSectionKeys(message) {
		if index != 0 {
			out.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(message.Sections[key])
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}
