package jsonparse

import (
	"encoding/json"
	"fmt"
)

// GrammarToolInputStream owns literal tool input and its incremental JSON
// representation. Prefix/equality decisions use UTF-16 units, not WTF-8 bytes.
type GrammarToolInputStream struct {
	Property string
	// PropertySet distinguishes a schema's empty key from the legacy input default.
	PropertySet     bool
	input           utf16Buffer
	started, closed bool
}

func (s *GrammarToolInputStream) property() string {
	if s.Property == "" && !s.PropertySet {
		return "input"
	}
	return s.Property
}
func (s *GrammarToolInputStream) Arguments() map[string]any {
	return map[string]any{s.property(): s.input.Value()}
}
func (s *GrammarToolInputStream) opening() (string, error) {
	if s.started {
		return "", nil
	}
	raw, err := json.Marshal(ArgumentText(s.property()))
	if err != nil {
		return "", err
	}
	s.started = true
	return "{" + string(raw) + ":\"", nil
}
func grammarFragment(text string) (string, error) {
	raw, err := json.Marshal(ArgumentText(text))
	if err != nil {
		return "", err
	}
	return string(raw[1 : len(raw)-1]), nil
}
func (s *GrammarToolInputStream) Append(delta string) (string, error) {
	if s.closed {
		return "", fmt.Errorf("grammar tool input for property %q changed after it was closed", s.property())
	}
	if delta == "" {
		return "", nil
	}
	if err := ValidateUTF16Key(delta); err != nil {
		return "", err
	}
	encoded, err := grammarFragment(delta)
	if err != nil {
		return "", err
	}
	opening, err := s.opening()
	if err != nil {
		return "", err
	}
	s.input.WriteText(delta)
	return opening + encoded, nil
}
func (s *GrammarToolInputStream) Close(final *string) (string, error) {
	before := previewUnits(s.input.Value())
	after := before
	if final != nil {
		var err error
		after, err = StringCodeUnits(*final)
		if err != nil {
			return "", err
		}
	}
	prefix := len(after) >= len(before)
	if prefix {
		for i, u := range before {
			if after[i] != u {
				prefix = false
				break
			}
		}
	}
	if s.closed {
		if final == nil || (prefix && len(after) == len(before)) {
			return "", nil
		}
		return "", fmt.Errorf("grammar tool input for property %q changed after it was closed", s.property())
	}
	if !prefix {
		return "", fmt.Errorf("grammar tool input for property %q changed non-monotonically", s.property())
	}
	var rest utf16Buffer
	for _, u := range after[len(before):] {
		rest.WriteUnit(u)
	}
	encoded, err := grammarFragment(previewText(rest.Value()))
	if err != nil {
		return "", err
	}
	opening, err := s.opening()
	if err != nil {
		return "", err
	}
	for _, u := range after[len(before):] {
		s.input.WriteUnit(u)
	}
	s.closed = true
	return opening + encoded + "\"}", nil
}
