package durable

import (
	"context"
	"testing"
	"time"
)

// Source codemode.test.ts:663. This direct real-QuickJS boundary control keeps
// null distinct from an absent delivery in both native invocation protocols.
// A separate production-path oracle is required for journal/reopen behavior.
func TestMainCodemodeQuickJSUndefinedDeliveryDistinctFromNull(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		name := "serial"
		if parallel {
			name = "parallel"
		}
		t.Run(name, func(t *testing.T) {
			sandbox, err := NewQuickJSSandbox(bg)
			if err != nil {
				t.Fatal(err)
			}
			defer sandbox.Close(bg)
			ctx, cancel := context.WithTimeout(bg, 5*time.Second)
			defer cancel()
			start := CodemodeStart{StartedAt: 1000, Seed: 1, ParallelCalls: parallel, DrainAcknowledgements: parallel, Globals: []CodemodeGlobalDescription{{Name: "host.nothing", Version: 1}, {Name: "host.null", Version: 1}}}
			vm, err := sandbox.Start(ctx, `const a=await host.nothing(null);const b=await host.null(null);return [a===undefined,b===null];`, start)
			if err != nil {
				t.Fatal(err)
			}
			defer vm.Close()
			seq := 0
			var pending *CodemodeCall
			deliver := func(call CodemodeCall) {
				t.Helper()
				seq++
				hasValue := call.Name == "host.null"
				if call.Name != "host.null" && call.Name != "host.nothing" {
					t.Fatal(call)
				}
				if err := vm.Deliver(ctx, CodemodeDelivery{Seq: seq, Now: 1000, Call: call, HasValue: hasValue, Value: nil}); err != nil {
					t.Fatal(err)
				}
			}
			for {
				event, err := vm.Next(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if event.Drain != nil {
					if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
						t.Fatal(err)
					}
					if pending != nil {
						deliver(*pending)
						pending = nil
					}
					continue
				}
				if event.Call != nil {
					if parallel {
						if pending != nil {
							t.Fatal("unexpected parallel calls")
						}
						pending = event.Call
					} else {
						deliver(*event.Call)
					}
					continue
				}
				if event.Result != nil {
					if event.Result.Error != "" || !equalJSONValue(event.Result.Value, []any{true, true}) {
						t.Fatalf("undefined/null result: %+v", event.Result)
					}
					return
				}
				t.Fatal("empty event")
			}
		})
	}
}
