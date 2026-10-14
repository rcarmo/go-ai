package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestQuickJS120SetupAdmissionBounds(t *testing.T) {
	callback := func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Error("invalid setup dispatched callback")
		return nil, nil
	}
	for _, opts := range []Options{{MemoryLimitBytes: -1}, {StartedAt: -1}, {Store: map[string]json.RawMessage{"key": json.RawMessage(`bad`)}}, {Store: map[string]json.RawMessage{"key": json.RawMessage(`"` + strings.Repeat("a", int(maxBridgeStringBytes)) + `"`)}}, {Tools: []Tool{{Name: "a", Execute: callback}, {Name: "b", JSName: "a", Execute: callback}}}, {Tools: []Tool{{Name: "a", JSName: "not-valid", Execute: callback}}}, {Globals: []Global{{Name: "one", Execute: callback}, {Name: "one", Execute: callback}}}} {
		if err := validateInputs("return 1", opts); err == nil {
			t.Fatal("invalid setup admitted", opts)
		}
	}
	if err := validateInputs(strings.Repeat("x", int(maxBridgeStringBytes)+1), Options{}); err == nil {
		t.Fatal("oversize source admitted")
	}
	if err := validateInputs("return 1", Options{Tools: []Tool{{Name: "mcp.leaf", JSName: "mcp_leaf", Execute: callback}}, Store: map[string]json.RawMessage{"null": json.RawMessage(`null`)}}); err != nil {
		t.Fatal(err)
	}
}
