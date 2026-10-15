package quickjs

import (
	"strings"
	"testing"
)

func TestQuickJS120MalformedBridgeReturnAndUnknownOperation(t *testing.T) {
	for _, operation := range []string{"", "nonsense", "watch:require", "DONE"} {
		if err := validateBridgeOperation(operation); err == nil {
			t.Fatal("unknown operation accepted", operation)
		}
	}
	for _, operation := range []string{"call", "global", "output", "done"} {
		if err := validateBridgeOperation(operation); err != nil {
			t.Fatal(operation, err)
		}
	}
	for _, payload := range []string{"", "{", "[1,", strings.Repeat(" ", int(maxBridgeStringBytes)+1) + "null"} {
		value, err := parseBridgeReturn(payload, true)
		if err == nil || value != nil {
			t.Fatal("invalid return accepted", len(payload), value)
		}
	}
	value, err := parseBridgeReturn("", false)
	if err != nil || value != nil {
		t.Fatal("undefined return", value, err)
	}
	for _, payload := range []string{"null", "42", `{"value":[1,true]}`} {
		value, err := parseBridgeReturn(payload, true)
		if err != nil || string(value) != payload {
			t.Fatal(payload, value, err)
		}
	}
}
