package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestCodemode120CustomGlobalJournalReopenAndRevocation(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		var effects atomic.Int64
		sandbox := &revokeSandbox120{sandbox: engine, ready: make(chan struct{})}
		registry := NewRegistry()
		def := taskDefinition(t, "custom-global-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const first=await host.lookup({n:1});if(first!==7)throw Error("lost global");let revoked=false;try{await host.lookup({n:2})}catch(e){revoked=true}return revoked;`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != true {
				t.Error(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{{Name: "host.lookup", Version: 1, Execute: func(_ context.Context, args any) (any, error) {
			effects.Add(1)
			if !equalJSONValue(args.(map[string]any)["n"], 1) {
				t.Error(args)
			}
			return 7, nil
		}}}})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		opts := h.options
		opts.CodemodeGlobals = nil
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), opts)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("%+v %+v effects=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load())
		}
	})
}
func TestCodemode120CustomGlobalReservedAdmission(t *testing.T) {
	callback := func(context.Context, any) (any, error) { return nil, nil }
	for _, name := range []string{"models.custom", "tools.custom", "text", "bad-name", "a.b.c", "Date", "Math.random", "Promise", "globalThis.lookup", "fetch", "__proto__.x"} {
		if err := validateCodemodeGlobals([]CodemodeGlobal{{Name: name, Version: 1, Execute: callback}}, DefaultLimits()); err == nil {
			t.Fatal("reserved global admitted", name)
		}
	}
}
