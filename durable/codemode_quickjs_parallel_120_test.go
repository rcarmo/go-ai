package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSUnsupportedParallelTerminates(t *testing.T) {
	for _, drains := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "drains"}[drains], func(t *testing.T) { testQuickJSUnsupportedParallel120(t, drains) })
	}
}
func testQuickJSUnsupportedParallel120(t *testing.T, drains bool) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	sandbox.AcknowledgeDrains = drains
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "parallel-rejected-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `await Promise.all([tools.codemode_leaf({n:1}),tools.codemode_leaf({n:2})]);store("unsafe",true);`, sandbox)
			if err != nil {
				return err
			}
			if drains && effects.Load() != 0 {
				t.Error("effect before certified drain", effects.Load())
			}
			if !strings.Contains(result.Error, "parallel") || len(result.StoreWrites) != 0 || effects.Load() > 1 {
				t.Errorf("parallel rejected result %+v effects=%d", result, effects.Load())
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
