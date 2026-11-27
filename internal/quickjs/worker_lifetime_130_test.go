package quickjs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestQuickJS130CapacityReleasedOnActualExit(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	for _, mode := range []string{"success", "script-error", "callback-error", "callback-panic", "abort"} {
		t.Run(mode, func(t *testing.T) {
			var reserved, released atomic.Int64
			var exited <-chan struct{}
			opts := Options{Timeout: 5 * time.Second, AcquireWorker: func() (func(), error) { reserved.Add(1); return func() { released.Add(1) }, nil }, OnWorkerStarted: func(done <-chan struct{}) error {
				exited = done
				select {
				case <-done:
					t.Error("exit signalled before start")
				default:
				}
				if released.Load() != 0 {
					t.Error("capacity released before start")
				}
				if mode == "callback-panic" {
					panic("private callback panic")
				}
				if mode == "callback-error" {
					return errors.New("private callback failure")
				}
				return nil
			}}
			code := `return 1;`
			ctx := context.Background()
			if mode == "script-error" {
				code = `throw Error("failure");`
			}
			if mode == "abort" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				opts.Tools = []Tool{{Name: "wait", Execute: func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
					cancel()
					<-ctx.Done()
					return nil, ctx.Err()
				}}}
				code = `await tools.wait({});`
			}
			result := engine.Execute(ctx, code, opts)
			if mode == "success" && result.Error != nil {
				t.Fatal(result.Error)
			}
			if mode != "success" && result.Error == nil {
				t.Fatal("expected failure", mode)
			}
			if reserved.Load() != 1 || released.Load() != 1 || exited == nil {
				t.Fatal(reserved.Load(), released.Load(), exited)
			}
			select {
			case <-exited:
			default:
				t.Fatal("Execute returned before exit")
			}
		})
	}
}

// Source: pi v1.7.0 packages/codemode/test/worker-lifetime.test.ts:34-51.
// The native failure point is a closed, precompiled WASM module: Node's worker
// constructor and Go's module instantiation are different hosts, but both fail
// after capacity admission and before the worker-start notification.
func TestSourceV170CapacityReleasedOnModuleConstructionFailure(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	if err := engine.compiled.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	var acquired, released, started int
	result := engine.Execute(context.Background(), `return 1`, Options{
		AcquireWorker: func() (func(), error) {
			acquired++
			return func() { released++ }, nil
		},
		OnWorkerStarted: func(<-chan struct{}) error {
			started++
			return nil
		},
	})
	if result.Error == nil || result.Error.Kind != ErrorSandbox || !strings.Contains(result.Error.Message, "Failed to start QuickJS:") {
		t.Errorf("construction failure=%+v", result.Error)
	}
	if acquired != 1 || released != 1 || started != 0 {
		t.Errorf("acquired=%d, released=%d, started=%d; want 1, 1 and 0", acquired, released, started)
	}
}

func TestQuickJS130CapacityFailureStartsNoVM(t *testing.T) {
	engine, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	started := false
	result := engine.Execute(context.Background(), `return 1;`, Options{AcquireWorker: func() (func(), error) { return nil, errors.New("full") }, OnWorkerStarted: func(<-chan struct{}) error { started = true; return nil }})
	if result.Error == nil || started {
		t.Fatal(result.Error, started)
	}
}
