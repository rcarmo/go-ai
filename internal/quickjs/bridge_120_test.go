package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120BridgeBudgetsBeforeHostEffect(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var effects atomic.Int64
	opts := Options{Timeout: 15 * time.Second, MemoryLimitBytes: 128 << 20, Tools: []Tool{{Name: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { effects.Add(1); return nil, nil }}}}
	for _, code := range []string{`await tools.leaf({x:"a".repeat(1100000)})`, `text("a".repeat(17000000))`, `return "a".repeat(17000000)`} {
		result := engine.Execute(context.Background(), code, opts)
		if result.OK || result.Error == nil || (result.Error.Kind != ErrorSandbox && !(result.Error.Kind == ErrorScript && strings.Contains(result.Error.Message, "output exceeded"))) {
			t.Fatal("bridge bound not enforced", result.Error)
		}
	}
	// Exercise the production admission boundary directly at the count ceiling;
	// 100000 VM host crossings obscure this assertion behind execution deadlines.
	boundary := vm{output: make([]OutputItem, maxBridgeOutputItems-1)}
	if !boundary.admitOutput(1) {
		t.Fatal("last output slot rejected")
	}
	boundary.output = append(boundary.output, OutputItem{Type: "text", Text: "x"})
	if boundary.admitOutput(1) {
		t.Fatal("output count ceiling not enforced")
	}
	if effects.Load() != 0 {
		t.Fatal("oversize host effect admitted", effects.Load())
	}
}
