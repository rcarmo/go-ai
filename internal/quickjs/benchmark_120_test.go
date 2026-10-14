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
