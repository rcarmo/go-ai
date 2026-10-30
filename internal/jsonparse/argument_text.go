package jsonparse

import (
	"bytes"
	"fmt"
)

// ArgumentText is a JSON string carrying a tool-argument fragment, not ordinary
// model text. Its native string representation retains isolated UTF-16 units.
// Providers must opt in on the exact argument-fragment field only.
type ArgumentText string

func (s *ArgumentText) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	value, err := ParseJSONValue(string(raw), true)
	if err != nil {
		return err
	}
	switch v := value.(type) {
	case string:
		*s = ArgumentText(v)
	case UTF16String:
		*s = ArgumentText(v.WTF8())
	default:
		return fmt.Errorf("tool argument fragment must be a JSON string")
	}
	return nil
}
func (s ArgumentText) MarshalJSON() ([]byte, error) {
	if err := ValidateUTF16Key(string(s)); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := WriteUTF16Key(&b, string(s)); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
