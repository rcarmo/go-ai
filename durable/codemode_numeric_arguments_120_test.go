package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSNumericArgumentsExactThroughJournalReopen(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		var tools, globals atomic.Int64
		check := func(value any) {
			number, ok := value.(json.Number)
			if !ok || number.String() != "1000000000000000100" {
				t.Errorf("numeric argument changed %T %v", value, value)
			}
		}
		registry := NewRegistry()
		leaf := wrapRegistration("numeric_leaf")
		leaf.Execute = func(_ context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
			tools.Add(1)
			check(args["n"])
			return ToolResult{Content: "ok"}, nil
		}
		if err = registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		sandbox := &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}
		def := taskDefinition(t, "numeric-args-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `await tools.numeric_leaf({n:1000000000000000100});await host.check({n:1000000000000000100});return "ok";`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != "ok" {
				t.Error(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, CodemodeGlobals: []CodemodeGlobal{{Name: "host.check", Version: 1, Execute: func(_ context.Context, args any) (any, error) {
			globals.Add(1)
			check(args.(map[string]any)["n"])
			return nil, nil
		}}}})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || tools.Load() != 1 || globals.Load() != 1 {
			t.Fatalf("%+v %+v tools=%d globals=%d", record.State.Outcome, record.State.Outcome.Error, tools.Load(), globals.Load())
		}
	})
}
