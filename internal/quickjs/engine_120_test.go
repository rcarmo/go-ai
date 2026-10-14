package quickjs

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestQuickJS120DeterministicClockRandomStoreAndTools(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	opts := Options{StartedAt: 123456000, Seed: 42, MemoryLimitBytes: 32 << 20, Store: map[string]json.RawMessage{"key": json.RawMessage(`3`)}, Tools: []Tool{{Name: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`7`), nil }}}}
	code := `text("hello");store("next",load("key")+1);return [Date.now(), Math.random(), await tools.leaf({})]`
	first := engine.Execute(context.Background(), code, opts)
	second := engine.Execute(context.Background(), code, opts)
	if !first.OK || !second.OK {
		t.Fatal(first.Error, second.Error)
	}
	var a, b []float64
	if err = json.Unmarshal(first.Value, &a); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(second.Value, &b); err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 || len(b) != 3 || a[0] != 123456000 || a[1] != b[1] || a[2] != 7 {
		t.Fatal(a, b)
	}
	opts.Seed = 43
	third := engine.Execute(context.Background(), `return [Math.random(), typeof WeakRef, typeof FinalizationRegistry]`, opts)
	var different []any
	if err = json.Unmarshal(third.Value, &different); err != nil || len(different) != 3 || different[0] == a[1] || different[1] != "undefined" || different[2] != "undefined" {
		t.Fatal("seed/GC intrinsics", third.Error, different, err)
	}
	if string(first.StoreWrites.Set["next"]) != "4" || len(first.Output) != 1 {
		t.Fatal(first)
	}
}
func TestQuickJS120HardLoopCancellationAndHeap(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	started := time.Now()
	result := engine.Execute(context.Background(), `for(;;){}`, Options{Timeout: 100 * time.Millisecond, MemoryLimitBytes: 8 << 20})
	if result.Error == nil || result.Error.Kind != ErrorTimeout || time.Since(started) > 3*time.Second {
		t.Fatal(result.Error, time.Since(started))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = engine.Execute(ctx, `for(;;){}`, Options{})
	if result.Error == nil || result.Error.Kind != ErrorAborted {
		t.Fatal(result.Error)
	}
	result = engine.Execute(context.Background(), `let a=[];for(;;)a.push(new Uint8Array(1048576));`, Options{Timeout: 2 * time.Second, MemoryLimitBytes: 8 << 20})
	if result.OK || result.Error == nil || result.Error.Kind == ErrorTimeout {
		t.Fatal("heap limit not enforced", result.Error)
	}
	// Closed/failed modules must not poison the compiled engine or next VM.
	for i := 0; i < 3; i++ {
		fresh := engine.Execute(context.Background(), `return 7;`, Options{Seed: 42, MemoryLimitBytes: 8 << 20})
		if !fresh.OK || string(fresh.Value) != "7" {
			t.Fatal("engine recovery after resource failure", fresh.Error)
		}
	}
}
