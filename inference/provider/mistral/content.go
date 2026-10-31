package mistral

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

type mistralContentPart struct {
	Type     string                `json:"type"`
	Text     string                `json:"text,omitempty"`
	Thinking []mistralThinkingText `json:"thinking,omitempty"`
}
type mistralThinkingText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type mistralContentDelta struct{ kind, text string }

// Decode UTF-16 only inside this wire boundary; publish sanitized scalar text,
// never the parser's lossless argument representation, to ordinary content.
func mistralContentDeltas(raw json.RawMessage) []mistralContentDelta {
	if len(raw) == 0 {
		return nil
	}
	var value any
	var err error
	// Ordinary scalar content needs no argument-domain parser. Escaped UTF-16
	// must stay lossless until joined thinking parts have been sanitized.
	if utf8.Valid(raw) && !bytes.Contains(raw, []byte(`\u`)) {
		err = json.Unmarshal(raw, &value)
	} else {
		value, err = jsonparse.ParseJSONValue(string(raw), false)
	}
	if err != nil {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		items = []any{value}
	}
	var out []mistralContentDelta
	for _, item := range items {
		if text, ok := mistralScalarText(item); ok {
			if text != "" {
				out = append(out, mistralContentDelta{"text", text})
			}
			continue
		}
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := object["type"].(string)
		text := ""
		switch kind {
		case "text":
			text, _ = mistralScalarText(object["text"])
		case "thinking":
			parts, _ := object["thinking"].([]any)
			var units []uint16
			for _, part := range parts {
				if object, ok := part.(map[string]any); ok {
					switch value := object["text"].(type) {
					case string:
						codes, _ := jsonparse.StringCodeUnits(value)
						units = append(units, codes...)
					case jsonparse.UTF16String:
						units = append(units, value.CodeUnits()...)
					}
				}
			}
			var joined strings.Builder
			for i := 0; i < len(units); i++ {
				u := units[i]
				if u >= 0xd800 && u <= 0xdbff {
					if i+1 < len(units) && units[i+1] >= 0xdc00 && units[i+1] <= 0xdfff {
						joined.WriteRune(utf16.DecodeRune(rune(u), rune(units[i+1])))
						i++
					}
					continue
				}
				if u >= 0xdc00 && u <= 0xdfff {
					continue
				}
				joined.WriteRune(rune(u))
			}
			text = joined.String()
		}
		if text != "" && (kind == "text" || kind == "thinking") {
			out = append(out, mistralContentDelta{kind, text})
		}
	}
	return out
}
func mistralScalarText(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return goai.SanitizeSurrogates(v), true
	case jsonparse.UTF16String:
		return goai.SanitizeSurrogates(v.WTF8()), true
	}
	return "", false
}
