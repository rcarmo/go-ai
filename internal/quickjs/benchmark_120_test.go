package quickjs

import (
	"context"
	"encoding/json"
	"testing"
)

func BenchmarkQuickJS120SerialStoreTool(b *testing.B) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close(context.Background())
	opts := Options{StartedAt: 1000, Seed: 42, SerialCalls: true, MemoryLimitBytes: 32 << 20, Store: map[string]json.RawMessage{"key": json.RawMessage(`3`)}, Tools: []Tool{{Name: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`7`), nil }}}}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := engine.Execute(context.Background(), `store("next",load("key")+1);text(await tools.leaf({}));return Date.now();`, opts)
		if !result.OK || string(result.Value) != "1000" || string(result.StoreWrites.Set["next"]) != "4" || len(result.Output) != 1 {
			b.Fatal(result)
		}
	}
}

func BenchmarkQuickJS120ParallelReusedEngine(b *testing.B) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close(context.Background())
	opts := Options{StartedAt: 1000, Seed: 42, InterruptBudget: 100000, MemoryLimitBytes: 32 << 20, Tools: []Tool{{Name: "leaf", Execute: func(context.Context, json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`7`), nil }}}}
	code := `const values=await Promise.all(Array.from({length:8},(_,n)=>tools.leaf({n})));store("sum",values.reduce((a,b)=>a+b,0));return load("sum");`
	// Warm module setup separately from measured execution; the engine compile
	// remains outside the benchmark timer and is not repeated per iteration.
	warm := engine.Execute(context.Background(), code, opts)
	if !warm.OK {
		b.Fatal(warm.Error)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		result := engine.Execute(context.Background(), code, opts)
		if !result.OK || string(result.Value) != "56" || string(result.StoreWrites.Set["sum"]) != "56" || len(result.Calls) != 8 {
			b.Fatal(result.Error, string(result.Value), result.StoreWrites)
		}
	}
}
