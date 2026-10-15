package quickjs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A failed done message must carry the guest's error object, never an arbitrary
// scalar or empty object. Script errors and malformed bridge errors stay distinct.
func parseScriptError(payload string) (*Error, error) {
	if len(payload) > int(maxBridgeStringBytes) {
		return nil, fmt.Errorf("script error exceeds bridge byte limit")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("script error is not an object")
	}
	var name, message, stack string
	if raw, ok := fields["name"]; ok && (string(raw) == "null" || json.Unmarshal(raw, &name) != nil || name == "") {
		return nil, fmt.Errorf("script error is malformed")
	}
	if raw, ok := fields["message"]; !ok || string(raw) == "null" || json.Unmarshal(raw, &message) != nil {
		return nil, fmt.Errorf("script error is malformed")
	}
	if raw, ok := fields["stack"]; ok {
		if string(raw) == "null" || json.Unmarshal(raw, &stack) != nil {
			return nil, fmt.Errorf("script error is malformed")
		}
	}
	// Retain only script frames even if a corrupted guest exposes prelude frames.
	lines := strings.Split(stack, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if !strings.Contains(line, "codemode-prelude.js") {
			kept = append(kept, line)
		}
	}
	return &Error{Kind: ErrorScript, Name: name, Message: message, Stack: strings.Join(kept, "\n")}, nil
}
