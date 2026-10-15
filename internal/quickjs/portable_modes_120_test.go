package quickjs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
)

func TestQuickJS120PortableInterpreterAndCompiler(t *testing.T) {
	for _, mode := range []string{"interpreter", "compiler"} {
		t.Run(mode, func(t *testing.T) {
			config := wazero.NewRuntimeConfigInterpreter()
			if mode == "compiler" {
				config = wazero.NewRuntimeConfigCompiler()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			engine, err := newEngineWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer engine.Close(context.Background())
			for _, script := range []struct {
				name, code string
				budget     int
				timeout    bool
			}{
				{"tools-globals-store", `const v=await tools.echo({n:3});const g=await host.sum(v.n,4);store('saved',g);console.log('console');return {g,clock:Date.now()};`, 100000, false},
				// A short poll budget also completes under race-instrumented WASM
				// interpretation before the independent wall deadline.
				{"interrupt", `while(true){}`, 32, true},
				{"budget-through-waits", `await tools.echo({n:1});await tools.echo({n:2});while(true){}`, 32, true},
			} {
				t.Run(script.name, func(t *testing.T) {
					toolCalls := 0
					result := engine.Execute(ctx, script.code, Options{InterruptBudget: script.budget, MemoryLimitBytes: 32 << 20, Timeout: 15 * time.Second, Clock: func() int64 { return 123 }, Seed: 7, OrderedSettlements: true, Tools: []Tool{{Name: "echo", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) { toolCalls++; return args, nil }}}, Globals: []Global{{Name: "host.sum", Spread: true, Execute: func(_ context.Context, _ json.RawMessage) (json.RawMessage, error) { return json.RawMessage(`7`), nil }}}})
					if script.name == "budget-through-waits" && toolCalls < 2 {
						t.Fatalf("budget test did not cross multiple host waits: %d", toolCalls)
					}
					if script.timeout {
						if result.Error == nil || result.Error.Kind != ErrorTimeout || !strings.Contains(result.Error.Message, "interrupt budget") {
							t.Fatal(result.Error)
						}
						return
					}
					if result.Error != nil || string(result.Value) != `{"g":7,"clock":123}` || string(result.StoreWrites.Set["saved"]) != "7" {
						t.Fatal(result, result.Error)
					}
				})
			}
		})
	}
}
