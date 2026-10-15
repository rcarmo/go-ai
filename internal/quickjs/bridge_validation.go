package quickjs

import (
	"encoding/json"
	"fmt"
)

func validateBridgeOperation(operation string) error {
	switch operation {
	case "call", "global", "output", "done":
		return nil
	default:
		return fmt.Errorf("unknown QuickJS bridge operation")
	}
}

func parseBridgeReturn(payload string, present bool) (json.RawMessage, error) {
	if !present {
		return nil, nil
	}
	if len(payload) > int(maxBridgeOutputBytes) || !json.Valid([]byte(payload)) {
		return nil, fmt.Errorf("invalid QuickJS return JSON")
	}
	return json.RawMessage(payload), nil
}
