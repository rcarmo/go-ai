package quickjs

import (
	"context"
	"encoding/json"
	"testing"
)

func TestQuickJS120GlobalCannotReplaceIntrinsicOrAmbientRoot(t *testing.T) {
	callback := func(context.Context, json.RawMessage) (json.RawMessage, error) {
		t.Error("reserved callback dispatched")
		return nil, nil
	}
	for _, name := range []string{"performance", "performance.now", "module", "WebAssembly", "InternalError", "Date", "Math.random", "Object", "Promise", "globalThis.x", "process", "fetch", "WeakRef", "__proto__.x"} {
		if err := validateInputs("return 1", Options{Globals: []Global{{Name: name, Execute: callback}}}); err == nil {
			t.Fatal("reserved global admitted", name)
		}
	}
	if err := validateInputs("return 1", Options{Globals: []Global{{Name: "host.lookup", Execute: callback}}}); err != nil {
		t.Fatal(err)
	}
}
