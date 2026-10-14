package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSAliasesAndHiddenTools(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		leaf := wrapRegistration("mcp.leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		hidden := wrapRegistration("pi.codemode.secret")
		hidden.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			t.Error("reserved tool exposed")
			return ToolResult{}, nil
		}
		if err := registry.Register(hidden); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "discovery-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			tools, err := r.CodemodeTools(ctx)
			if err != nil {
				return err
			}
			if len(tools) != 1 || tools[0].Name != "mcp.leaf" {
				t.Error("hidden catalogue", tools)
			}
			result, err := r.RunCodemode(ctx, "run", `if(ALL_TOOLS.length!==1||ALL_TOOLS[0].name!=="mcp_leaf")throw Error("catalogue");if("pi_codemode_secret" in tools||"pi.codemode.secret" in tools)throw Error("hidden");text(await tools["mcp.leaf"]({}));text(await tools.mcp_leaf({}));`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || effects.Load() != 2 {
				t.Errorf("aliases %+v effects=%d", result, effects.Load())
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
