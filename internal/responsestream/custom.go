package responsestream

import (
	"encoding/json"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// CustomInput keeps the initial encoded opening unsent until the first delta
// or closure. Literal input remains schema-owned and outside SDK snapshots.
type CustomInput struct {
	stream jsonparse.GrammarToolInputStream
	unsent string
}

func NewCustomInput(raw []byte, property string) (*CustomInput, error) {
	var item struct {
		Input jsonparse.ArgumentText `json:"input"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	s := &CustomInput{stream: jsonparse.GrammarToolInputStream{Property: property}}
	delta, err := s.stream.Append(string(item.Input))
	if err != nil {
		return nil, err
	}
	s.unsent = delta
	return s, nil
}
func (s *CustomInput) Arguments() map[string]any { return s.stream.Arguments() }
func (s *CustomInput) flush(delta string) string {
	result := s.unsent + delta
	s.unsent = ""
	return result
}
func (s *CustomInput) Append(raw []byte) (string, error) {
	var wire struct {
		Delta jsonparse.ArgumentText `json:"delta"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return "", err
	}
	delta, err := s.stream.Append(string(wire.Delta))
	if err != nil {
		return "", err
	}
	return s.flush(delta), nil
}
func (s *CustomInput) Close(raw []byte) (string, error) {
	var wire struct {
		Input     *jsonparse.ArgumentText `json:"input"`
		Arguments *jsonparse.ArgumentText `json:"arguments"`
		Delta     *jsonparse.ArgumentText `json:"delta"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return "", err
	}
	value := wire.Input
	if value == nil {
		value = wire.Arguments
	}
	if value == nil {
		value = wire.Delta
	}
	var final *string
	if value != nil {
		text := string(*value)
		final = &text
	}
	delta, err := s.stream.Close(final)
	if err != nil {
		return "", err
	}
	return s.flush(delta), nil
}
