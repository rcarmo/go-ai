package durable

import (
	"bytes"
	"encoding/json"
	"io"

	goai "github.com/rcarmo/go-ai"
)

// validateContent enforces the Go representation of the original message union.
// There is one wire property, content, never a second persisted string field.
func (r MessageReceipt) validateContent() error {
	if r.ContentText != nil {
		if r.Content != nil {
			return reject("message content has both string and block arms")
		}
		if r.Role != goai.RoleUser && r.Role != goai.RoleSystem {
			return reject("string content requires user or system role")
		}
	}
	return nil
}

func (r MessageReceipt) MarshalJSON() ([]byte, error) {
	limits := maximumDurableLimits()
	return encodeBounded(r, limits, limits.MaxRecordBytes)
}

func (r *MessageReceipt) UnmarshalJSON(data []byte) error {
	// Shadow content with its raw wire union; all remaining fields retain the
	// receipt schema. Alias conversion removes methods, not schema validation.
	type plain MessageReceipt
	value := plain{}
	wire := struct {
		*plain
		Content      json.RawMessage `json:"content"`
		ErrorMessage json.RawMessage `json:"errorMessage"`
	}{plain: &value}
	decode := func(data []byte, target any) error {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(target); err != nil {
			return err
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return reject("trailing message JSON")
		}
		return nil
	}
	if err := decode(data, &wire); err != nil {
		return err
	}
	if len(wire.ErrorMessage) != 0 && !bytes.Equal(bytes.TrimSpace(wire.ErrorMessage), []byte("null")) {
		if err := decode(wire.ErrorMessage, &value.ErrorMessage); err != nil {
			return err
		}
		value.ErrorMessagePresent = value.ErrorMessage == ""
	}
	content := bytes.TrimSpace(wire.Content)
	if len(content) > 0 && content[0] == '"' {
		var text string
		if err := decode(content, &text); err != nil {
			return err
		}
		value.ContentText = &text
	} else if len(content) > 0 {
		if err := decode(content, &value.Content); err != nil {
			return err
		}
	}
	result := MessageReceipt(value)
	if err := result.validateContent(); err != nil {
		return err
	}
	*r = result
	return nil
}
