package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// The standalone alias fallback must not reach caller-controlled starts.
func TestV161CodemodeQuickJSStartRetainsStrictAliases(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	sandbox, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	var reservations atomic.Int64
	sandbox.AcquireWorker = func() (func(), error) {
		reservations.Add(1)
		return func() {}, nil
	}
	vm, err := sandbox.Start(ctx, `return await tools.a_b(1)`, CodemodeStart{
		StartedAt: 1000, Seed: 1,
		Tools: []CodemodeToolDescription{{Name: "a-b", Identifier: "a_b"}, {Name: "a_b", Identifier: "a_b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	got, err := vm.Next(ctx)
	if err != nil || got.Call != nil || got.Drain != nil || got.Result == nil || got.Result.Error != "sandbox: invalid or oversized QuickJS setup input" || got.Result.HasValue {
		t.Fatalf("start = %+v, %v; want strict setup refusal with no call or value", got, err)
	}
	if reservations.Load() != 0 {
		t.Fatal("rejected start reserved a worker")
	}
}
