// Package jsonparse provides incremental JSON parsing for streaming tool call arguments.
package jsonparse

import (
	"encoding/json"
	"strings"
)

// ParsePartialJSON attempts to parse a potentially incomplete JSON string
// by closing any open braces/brackets. Returns the parsed value and
// whether it succeeded.
func ParsePartialJSON(partial string) (map[string]interface{}, bool) {
	s := strings.TrimSpace(partial)
	if s == "" {
		return nil, false
	}

	// Try as-is first
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(s), &result); err == nil {
		return result, true
	}

	// Close open structures
	closed := closeJSON(s)
	if err := json.Unmarshal([]byte(closed), &result); err == nil {
		return result, true
	}

	return nil, false
}

// closeJSON attempts to close an incomplete JSON string.
func closeJSON(s string) string {
	var stack []byte
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	// If we're inside a string, close it
	if inString {
		s += `"`
	}

	// Close all open structures in reverse order
	for i := len(stack) - 1; i >= 0; i-- {
		s += string(stack[i])
	}

	return s
}
