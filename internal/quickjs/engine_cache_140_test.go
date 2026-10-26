package quickjs

import (
	"context"
	"testing"
)

func BenchmarkQuickJS140IsolatedEngineStartup(b *testing.B) {
	ctx := context.Background()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		engine, err := NewEngine(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if err := engine.Close(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

func TestQuickJS140EngineClosePreservesSiblingIsolation(t *testing.T) {
	ctx := context.Background()
	first, err := NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close(ctx) })
	second, err := NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close(ctx) })
	result := first.Execute(ctx, `globalThis.sentinel="secret";store.sentinel="secret";return true;`, Options{})
	if !result.OK {
		t.Fatal(result.Error)
	}
	if err := first.Close(ctx); err != nil {
		t.Fatal(err)
	}
	result = second.Execute(ctx, `return [typeof globalThis.sentinel,store.sentinel];`, Options{})
	if !result.OK || string(result.Value) != `["undefined",null]` {
		t.Fatal(result.Error, string(result.Value))
	}
}
