package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestNestedTool120ParentCallAttribution(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		leaf := wrapRegistration("leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "leaf"}, nil }
		middle := wrapRegistration("middle")
		middle.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			return api.CallTool(ctx, "inner", "leaf", JSON{})
		}
		outer := wrapRegistration("outer")
		outer.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			return api.CallTool(ctx, "middle-key", "middle", JSON{})
		}
		for _, reg := range []ToolRegistration{leaf, middle, outer} {
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
		}
		seen := map[string]ToolInvocation{}
		extension := &Extension{Name: "parent-attribution", ToolHooks: ToolHooks{BeforeTool: func(ctx context.Context, call goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
			identity, err := api.ToolInvocation(ctx)
			if err != nil {
				return nil, err
			}
			seen[call.Name] = identity
			return nil, nil
		}}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		options := Options{Registry: registry}
		def := taskDefinition(t, "parent-call-fixture", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.CallTool(ctx, "outer-key", "outer", JSON{}); err != nil {
				return err
			}
			if seen["middle"].ParentCallID != "outer-key" || seen["leaf"].ParentCallID != "outer-key/middle-key" || seen["leaf"].ParentTask != seen["middle"].Task {
				t.Error("nested parent attribution", seen)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, options)
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
