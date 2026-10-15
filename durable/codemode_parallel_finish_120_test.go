package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCodemode120ParallelFinishAbortsNestedCalls(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "error"}[fail], func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				sandbox, err := NewQuickJSSandbox(bg)
				if err != nil {
					t.Fatal(err)
				}
				defer sandbox.Close(bg)
				sandbox.ParallelCalls = true
				registry := NewRegistry()
				entered := make(chan struct{})
				var effects atomic.Int64
				slow := wrapRegistration("codemode_slow")
				slow.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
					effects.Add(1)
					close(entered)
					<-ctx.Done()
					return ToolResult{}, ctx.Err()
				}
				if err := registry.Register(slow); err != nil {
					t.Fatal(err)
				}
				fast := wrapRegistration("codemode_fast")
				fast.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
					select {
					case <-entered:
						return ToolResult{Content: "ok"}, nil
					case <-ctx.Done():
						return ToolResult{}, ctx.Err()
					}
				}
				if err := registry.Register(fast); err != nil {
					t.Fatal(err)
				}
				def := taskDefinition(t, "parallel-finish-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					code := `tools.codemode_slow({});await tools.codemode_fast({});return "done";`
					if fail {
						code = `tools.codemode_slow({});await tools.codemode_fast({});store("unsafe",true);throw Error("terminal");`
					}
					result, err := r.RunCodemode(ctx, "run", code, sandbox)
					if err != nil {
						return err
					}
					if fail && !strings.Contains(result.Error, "terminal") {
						return reject("terminal error missing")
					}
					if fail && len(result.StoreWrites) != 0 {
						t.Error("failed script exposed store writes", result.StoreWrites)
					}
					if !fail && result.Error != "" {
						return reject(result.Error)
					}
					again, err := r.RunCodemode(ctx, "run", code, sandbox)
					if err != nil {
						return err
					}
					if again.Error != result.Error || effects.Load() != 1 {
						t.Error("settled rerun duplicated effect", effects.Load(), again)
					}
					children, err := r.OwnedTasks(ctx)
					if err != nil {
						return err
					}
					if len(children) != 0 {
						t.Error("unfinished codemode children", children)
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
		})
	}
}
