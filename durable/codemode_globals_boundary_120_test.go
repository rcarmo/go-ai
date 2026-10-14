package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120CustomGlobalSpreadPanicAndJournalIsolation(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		var calls, panics atomic.Int64
		sandbox := &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}
		registry := NewRegistry()
		def := taskDefinition(t, "global-boundary-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const n=await host.sum({n:1},2);let caught=false;try{await host.fail(null)}catch(e){if(e.message.includes("private"))throw Error("private leak");caught=true}if(!caught)throw Error("panic uncaught");return n;`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || !equalJSONValue(result.Value, 3) {
				t.Error(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{{Name: "host.sum", Version: 1, Spread: true, Execute: func(_ context.Context, args any) (any, error) {
			calls.Add(1)
			values := args.([]any)
			if len(values) != 2 || !equalJSONValue(values[0].(map[string]any)["n"], 1) {
				t.Error(args)
			}
			values[0].(map[string]any)["n"] = 999
			return 3, nil
		}}, {Name: "host.fail", Version: 1, Execute: func(context.Context, any) (any, error) { panics.Add(1); panic("private host data") }}}})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeJournalKind {
				var journal codemodeJournalState
				if err = fromObject(doc.Value, &journal, h.session.limits); err != nil {
					t.Fatal(err)
				}
				args := journal.Issued[0].Arguments["args"].([]any)
				if !equalJSONValue(args[0].(map[string]any)["n"], 1) || strings.Contains(journal.Deliveries[1].Error, "private") {
					t.Fatal("journal alias/private leak", journal)
				}
			}
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || calls.Load() != 1 || panics.Load() != 1 {
			t.Fatalf("%+v %+v calls=%d panics=%d", record.State.Outcome, record.State.Outcome.Error, calls.Load(), panics.Load())
		}
	})
}
