package quickjs

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:670–694.
func TestSourceV160QuickJSSandboxLifetime(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	t.Run("parallel", func(t *testing.T) {
		scripts := []string{
			`globalThis.shared = 'a'; await null; return globalThis.shared`,
			`globalThis.shared = 'b'; await null; return globalThis.shared`,
			`return typeof globalThis.shared`,
		}
		started := make(chan struct{}, len(scripts))
		release := make(chan struct{})
		opts := Options{OnWorkerStarted: func(<-chan struct{}) error {
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}}
		type finished struct {
			index  int
			result Result
		}
		results := make(chan finished, len(scripts))
		for i, script := range scripts {
			go func() { results <- finished{i, engine.Execute(ctx, script, opts)} }()
		}
		for range scripts {
			select {
			case <-started:
			case got := <-results:
				t.Fatalf("execution %d ended before the three workers overlapped: %v", got.index, got.result.Error)
			case <-ctx.Done():
				t.Fatal("three workers did not overlap:", ctx.Err())
			}
		}
		close(release)
		values := make([]string, len(scripts))
		for range scripts {
			select {
			case got := <-results:
				if !got.result.OK {
					t.Fatalf("execution %d failed: %v", got.index, got.result.Error)
				}
				values[got.index] = string(got.result.Value)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		if !reflect.DeepEqual(values, []string{`"a"`, `"b"`, `"undefined"`}) {
			t.Fatalf("parallel values: %q; want a, b and undefined", values)
		}
	})
	t.Run("recursion", func(t *testing.T) {
		got := engine.Execute(ctx, `
			let depth = 0;
			function dive() { depth++; dive(); }
			try { dive(); } catch (error) { return [error.name, depth > 1000]; }
		`, Options{MemoryLimitBytes: 32 << 20})
		if !got.OK || string(got.Value) != `["RangeError",true]` {
			t.Fatalf("recursion ok=%v value=%s error=%+v; want catchable RangeError beyond 1000", got.OK, got.Value, got.Error)
		}
	})
	t.Run("closed", func(t *testing.T) {
		if err := engine.Close(ctx); err != nil {
			t.Fatal(err)
		}
		got := engine.Execute(ctx, "return 1", Options{})
		if got.OK || got.Error == nil || got.Error.Kind != ErrorSandbox || !strings.Contains(got.Error.Message, "closed") {
			t.Fatalf("execute after close: ok=%v value=%s error=%+v; want closed", got.OK, got.Value, got.Error)
		}
	})
}
