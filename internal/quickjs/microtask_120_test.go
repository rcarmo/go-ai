package quickjs

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS120MicrotaskLoopDeadlineAndRecovery(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	start := time.Now()
	result := engine.Execute(context.Background(), `while(true)await Promise.resolve();`, Options{Timeout: 100 * time.Millisecond, MemoryLimitBytes: 8 << 20})
	if result.OK || result.Error == nil || result.Error.Kind != ErrorTimeout || time.Since(start) > 3*time.Second {
		t.Fatal("microtask deadline", result.Error, time.Since(start))
	}
	fresh := engine.Execute(context.Background(), `return 7;`, Options{})
	if !fresh.OK || string(fresh.Value) != "7" {
		t.Fatal("microtask recovery", fresh.Error)
	}
}
func TestQuickJS120ExitStopsQueuedEffects(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var effects atomic.Int64
	result := engine.Execute(context.Background(), `Promise.resolve().then(()=>tools.leaf({}));exit("finished");`, Options{Tools: []Tool{{Name: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { effects.Add(1); return nil, nil }}}})
	if !result.OK || effects.Load() != 0 {
		t.Fatal("post-exit effect", result.Error, effects.Load())
	}
}
