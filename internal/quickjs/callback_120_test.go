package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestQuickJS120CallbackPanicAndResultBounds(t *testing.T) {
	for _, callback := range []func(context.Context, json.RawMessage) (json.RawMessage, error){func(context.Context, json.RawMessage) (json.RawMessage, error) { panic("private host data") }, func(context.Context, json.RawMessage) (json.RawMessage, error) {
		return make(json.RawMessage, int(maxBridgeStringBytes)+1), nil
	}} {
		value, err := boundedQuickJSCallback(context.Background(), callback, nil)
		if err == nil || value != nil || strings.Contains(err.Error(), "private") {
			t.Fatal(value, err)
		}
	}
}
