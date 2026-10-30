package jsonparse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ArgumentsReplacement distinguishes an empty continuation delta from a reset.
// Delta and Text use Go strings normally, UTF16String for isolated code units.
type ArgumentsReplacement struct {
	Delta        any
	Continuation bool
	Arguments    map[string]any
}

// ToolCallArgumentsStream owns raw text separately from partial arguments. It is
// not a content block and must not be persisted as one. Use the preview adapters
// for wire serialisation; ordinary persisted JSON validators remain unchanged.
type ToolCallArgumentsStream struct {
	text      utf16Buffer
	parser    StreamingJSONParser
	arguments map[string]any
	revision  uint64
}

func (s *ToolCallArgumentsStream) Text() any { return s.text.Value() }
func (s *ToolCallArgumentsStream) Append(delta string) map[string]any {
	s.text.WriteText(delta)
	s.parser.Append(delta)
	return s.partial()
}
func (s *ToolCallArgumentsStream) AppendUTF16(delta []uint16) map[string]any {
	for _, u := range delta {
		s.text.WriteUnit(u)
	}
	s.parser.AppendUTF16(delta)
	return s.partial()
}
func (s *ToolCallArgumentsStream) partial() map[string]any {
	if s.arguments == nil {
		s.arguments = map[string]any{}
	}
	value, _ := s.parser.Snapshot()
	if s.revision == s.parser.revision {
		return s.arguments
	}
	s.revision = s.parser.revision
	if object, ok := value.(map[string]any); ok {
		s.arguments = object
	} else {
		s.arguments = map[string]any{}
	}
	return s.arguments
}
func (s *ToolCallArgumentsStream) Replace(text string) ArgumentsReplacement {
	units, err := StringCodeUnits(text)
	if err != nil { // Invalid Go bytes are not JS string units; normalise as Append does.
		var b utf16Buffer
		b.WriteText(text)
		units = previewUnits(b.Value())
	}
	return s.ReplaceUTF16(units)
}
func (s *ToolCallArgumentsStream) ReplaceUTF16(units []uint16) ArgumentsReplacement {
	before := previewUnits(s.text.Value())
	continuation := len(units) >= len(before)
	if continuation {
		for i, u := range before {
			if units[i] != u {
				continuation = false
				break
			}
		}
	}
	if continuation {
		delta := units[len(before):]
		if len(delta) == 0 {
			return ArgumentsReplacement{Delta: "", Continuation: true, Arguments: s.partial()}
		}
		var b utf16Buffer
		for _, u := range delta {
			b.WriteUnit(u)
		}
		return ArgumentsReplacement{Delta: b.Value(), Continuation: true, Arguments: s.AppendUTF16(delta)}
	}
	*s = ToolCallArgumentsStream{}
	return ArgumentsReplacement{Arguments: s.AppendUTF16(units)}
}
func previewUnits(value any) []uint16 {
	switch v := value.(type) {
	case string:
		u, _ := StringCodeUnits(v)
		return u
	case UTF16String:
		return v.CodeUnits()
	default:
		panic("not a preview string")
	}
}
func previewText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case UTF16String:
		if v.high == 0 {
			return v.prefix
		}
		return v.prefix + surrogateBytes(v.high)
	default:
		panic("not a preview string")
	}
}

// Finish performs one exact structural parse (with the same string repairs as
// the completed-call helper). It never accepts a merely repairable prefix.
func (s *ToolCallArgumentsStream) Finish() (map[string]any, string) {
	return parseStreamingArguments(previewText(s.text.Value()))
}
func parseStreamingArguments(raw string) (map[string]any, string) {
	if strings.TrimFunc(raw, jsTrimSpace) == "" {
		return map[string]any{}, ""
	}
	parse := func(text string) (map[string]any, error) {
		if _, err := StringCodeUnits(text); err != nil {
			return nil, err
		}
		var syntax json.RawMessage
		if err := json.Unmarshal([]byte(text), &syntax); err != nil {
			return nil, err
		}
		parser := StreamingJSONParser{}
		parser.Append(text)
		value, present := parser.Snapshot()
		object, ok := value.(map[string]any)
		if !present || !ok {
			return nil, fmt.Errorf("tool call arguments must be a JSON object")
		}
		return object, nil
	}
	value, err := parse(raw)
	if err == nil {
		return value, ""
	}
	if repaired, repairErr := parse(repairToolJSON(raw)); repairErr == nil {
		return repaired, ""
	}
	return map[string]any{}, fmt.Sprintf("%s\n%s", err, raw)
}
func jsTrimSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// ParseJSONValue validates exact JSON grammar and preserves UTF-16 strings.
// preserveNumbers retains integer/decimal spellings for durable DTO decoding.
func ParseJSONValue(text string, preserveNumbers bool) (any, error) {
	if err := ValidateUTF16Key(text); err != nil {
		return nil, err
	}
	var syntax json.RawMessage
	if err := json.Unmarshal([]byte(text), &syntax); err != nil {
		return nil, err
	}
	p := StreamingJSONParser{preserveNumbers: preserveNumbers}
	p.Append(text + " ")
	value, present := p.Snapshot()
	if !present {
		return nil, fmt.Errorf("missing JSON value")
	}
	return value, nil
}
