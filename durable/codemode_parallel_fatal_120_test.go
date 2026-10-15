package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120ParallelInitialErrorSettlesWithoutEffects(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		sandbox, err := NewQuickJSSandbox(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer sandbox.Close(bg)
		sandbox.ParallelCalls = true
		var effects atomic.Int64
		registry := NewRegistry()
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "unsafe"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "fatal-initial-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			for i := 0; i < 2; i++ {
				result, err := r.RunCodemode(ctx, "run", `tools.codemode_leaf({});store("unsafe",true);throw Error("initial fatal");`, sandbox)
				if err != nil {
					return err
				}
				if !strings.Contains(result.Error, "initial fatal") || len(result.StoreWrites) != 0 || effects.Load() != 0 {
					t.Error("initial fatal admitted effects", result, effects.Load())
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
