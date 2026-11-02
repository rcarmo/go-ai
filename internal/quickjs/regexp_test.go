package quickjs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func TestRegExpUnicodeOracles(t *testing.T) {
	var f struct {
		Literal, Derived []struct {
			Name  string
			Input map[string]interface{}
			Want  string
		}
		Dependency []struct {
			Name  string
			Input interface{}
			Valid bool
		}
	}
	data, err := os.ReadFile("../../tests/testdata/upstream-main/validation-regex.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	e, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	count := 0
	check := func(name, pattern string, want bool) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := e.CheckRegExp(ctx, pattern)
			if err != nil || got != want {
				t.Fatalf("pattern=%q got=%v want=%v err=%v", pattern, got, want, err)
			}
		})
		count++
	}
	for _, c := range f.Literal {
		check(c.Name, c.Input["r"].(string), false)
	}
	for _, c := range f.Derived {
		check(c.Name, c.Input["v"].(string), strings.HasPrefix(c.Want, "ok "))
	}
	for _, c := range f.Dependency {
		if pattern, ok := c.Input.(string); ok {
			check(c.Name, pattern, c.Valid)
		}
	}
	if count != 45 {
		t.Fatalf("unexpected string oracle count %d", count)
	}
}

func TestRegExpFreshConcurrentModules(t *testing.T) {
	e, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			pattern, want := "(?<x>a)\\k<x>", true
			if i%2 == 0 {
				pattern, want = "(?<x>a)(?<x>b)", false
			}
			got, err := e.CheckRegExp(ctx, pattern)
			if err != nil || got != want {
				t.Errorf("run %d got=%v want=%v err=%v", i, got, want, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestRegExpCancellationDuringConstructorAndRecovery(t *testing.T) {
	type cancelKey struct{}
	var reached atomic.Bool
	factory := experimental.FunctionListenerFactoryFunc(func(def api.FunctionDefinition) experimental.FunctionListener {
		for _, name := range def.ExportNames() {
			if name == "qjs_call" {
				return experimental.FunctionListenerFunc(func(ctx context.Context, _ api.Module, _ api.FunctionDefinition, _ []uint64, _ experimental.StackIterator) {
					if cancel, ok := ctx.Value(cancelKey{}).(context.CancelFunc); ok {
						reached.Store(true)
						cancel()
					}
				})
			}
		}
		return nil
	})
	// Instrumented code must not enter the process compilation cache: wazero
	// reuses the first compiled listener closure across repeated factories.
	// Keep the real guest, host module and production runtime fences, but own
	// this test's compiler so -count repetitions observe their own witness.
	instrumented := experimental.WithFunctionListenerFactory(context.Background(), factory)
	r := wazero.NewRuntimeWithConfig(instrumented, wazero.NewRuntimeConfig().WithCloseOnContextDone(true).WithMemoryLimitPages(memoryLimitPages))
	defer r.Close(context.Background())
	if _, err := wasi_snapshot_preview1.Instantiate(instrumented, r); err != nil {
		t.Fatal(err)
	}
	if _, err := hostModule(r).Instantiate(instrumented); err != nil {
		t.Fatal(err)
	}
	compiled, err := r.CompileModule(instrumented, quickjsWasm)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{runtime: r, compiled: compiled}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = context.WithValue(ctx, cancelKey{}, context.CancelFunc(cancel))
	got, err := e.CheckRegExp(ctx, "a")
	if !reached.Load() || got || !errors.Is(err, context.Canceled) {
		t.Fatalf("constructor cancellation reached=%v got=%v err=%v", reached.Load(), got, err)
	}
	got, err = e.CheckRegExp(context.Background(), "(?<=a)b")
	if err != nil || !got {
		t.Fatalf("recovery got=%v err=%v", got, err)
	}
}

func TestRegExpInputIsDataAndClosedEngine(t *testing.T) {
	e, err := NewEngine(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, pattern := range []string{`x");for(;;){}//`, "[", `(?<x>a)(?<x>b)`} {
		got, err := e.CheckRegExp(ctx, pattern)
		if err != nil || got {
			t.Fatalf("pattern data %q got=%v err=%v", pattern, got, err)
		}
	}
	if got, err := e.CheckRegExp(ctx, `^(a+)+$`); err != nil || !got {
		t.Fatal("syntax check ran a match", got, err)
	}
	//lint:ignore SA1012 Deliberately invalid input verifies the nil-context rejection fence.
	if _, err := e.CheckRegExp(nil, "a"); err == nil {
		t.Fatal("nil context accepted")
	}
	e.Close(context.Background())
	if got, err := e.CheckRegExp(ctx, "a"); got || err == nil {
		t.Fatal("closed engine accepted", got, err)
	}
	var missing *Engine
	if got, err := missing.CheckRegExp(ctx, "a"); got || err == nil {
		t.Fatal("nil engine accepted", got, err)
	}
}
