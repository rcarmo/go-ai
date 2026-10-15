package jsonparse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseToolCallArguments parses a completed call, never a truncated prefix.
// Benign malformed string escapes/control characters are repaired, but the
// completed result must still be a JSON object.
func ParseToolCallArguments(raw string) (map[string]interface{}, string) {
	if strings.TrimSpace(raw) == "" {
		return map[string]interface{}{}, ""
	}
	parse := func(text string) (map[string]interface{}, error) {
		var value any
		if err := json.Unmarshal([]byte(text), &value); err != nil {
			return nil, err
		}
		object, ok := value.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("tool call arguments must be a JSON object")
		}
		return object, nil
	}
	value, err := parse(raw)
	if err == nil {
		return value, ""
	}
	if value, repairedErr := parse(repairToolJSON(raw)); repairedErr == nil {
		return value, ""
	}
	return map[string]interface{}{}, fmt.Sprintf("%s\n%s", err, raw)
}

func repairToolJSON(text string) string {
	var out strings.Builder
	inside := false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if !inside {
			out.WriteByte(c)
			if c == '"' {
				inside = true
			}
			continue
		}
		if c == '"' {
			inside = false
			out.WriteByte(c)
			continue
		}
		if c == '\\' {
			if i+1 < len(text) {
				next := text[i+1]
				valid := strings.ContainsRune(`"\/bfnrt`, rune(next))
				if next == 'u' && i+5 < len(text) {
					valid = true
					for j := i + 2; j < i+6; j++ {
						h := text[j]
						if !(h >= '0' && h <= '9' || h >= 'a' && h <= 'f' || h >= 'A' && h <= 'F') {
							valid = false
							break
						}
					}
				}
				if valid {
					out.WriteByte(c)
					out.WriteByte(next)
					i++
					continue
				}
			}
			out.WriteString(`\\`)
			continue
		}
		if c < 0x20 {
			fmt.Fprintf(&out, `\u%04x`, c)
		} else {
			out.WriteByte(c)
		}
	}
	return out.String()
}
