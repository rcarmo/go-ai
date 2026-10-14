package quickjs

import (
	"context"
	"encoding/json"
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
	opts := Options{Timeout: 3 * time.Second, MemoryLimitBytes: 32 << 20, Tools: []Tool{{Name: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { effects.Add(1); return nil, nil }}}}
	for _, code := range []string{`await tools.leaf({x:"a".repeat(1100000)})`, `text("a".repeat(1100000))`, `for(let i=0;i<5000;i++)text("x")`, `return "a".repeat(1100000)`} {
		result := engine.Execute(context.Background(), code, opts)
		if result.OK || result.Error == nil || result.Error.Kind != ErrorSandbox {
			t.Fatal("bridge bound not enforced", result.Error)
		}
	}
	if effects.Load() != 0 {
		t.Fatal("oversize host effect admitted", effects.Load())
	}
}
