package quickjs

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestQuickJS120AmbientCapabilitiesAndFrozenBuiltins(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	opts := Options{StartedAt: 123456000, Seed: 42, Timeout: 3 * time.Second, MemoryLimitBytes: 16 << 20}
	result := engine.Execute(context.Background(), `return [typeof process,typeof require,typeof fetch,typeof XMLHttpRequest,typeof WebSocket,typeof setTimeout,typeof std,typeof os,typeof __bridge,typeof WeakRef,typeof FinalizationRegistry,Object.isFrozen(Math),Object.isFrozen(JSON),Object.isFrozen(Object.prototype),Date.now(),new Date().getTime(),new Date().getTimezoneOffset()];`, opts)
	if !result.OK {
		t.Fatal(result.Error)
	}
	var values []any
	if err = json.Unmarshal(result.Value, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 17 {
		t.Fatal(values)
	}
	for i := 0; i < 11; i++ {
		if values[i] != "undefined" {
			t.Fatal("ambient capability", i, values)
		}
	}
	for i := 11; i < 14; i++ {
		if values[i] != true {
			t.Fatal("mutable builtin", i, values)
		}
	}
	if values[14] != float64(opts.StartedAt) || values[15] != float64(opts.StartedAt) || values[16] != float64(0) {
		t.Fatal("ambient time", values)
	}
	result = engine.Execute(context.Background(), `return await import("file:///etc/passwd");`, opts)
	if result.OK || result.Error == nil || result.Error.Kind == ErrorTimeout {
		t.Fatal("module load not denied promptly", result.Error)
	}
}
