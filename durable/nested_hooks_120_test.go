package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestNestedTool120HooksRewriteBlockAndReplaceDetachedResults(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "rewrite"
		if blocked {
			name = "block"
		}
		t.Run(name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var before, after, runs atomic.Int64
				reg := wrapRegistration("nested_hook")
				reg.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)
				reg.Execute = func(_ context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
					runs.Add(1)
					if args["n"] != json.Number("2") {
						t.Fatal("hook rewrite missing", args)
					}
					return ToolResult{Content: "original", Details: JSON{"owned": true}}, nil
				}
				ext := &Extension{Name: "nested-hooks", Tools: []ToolRegistration{reg}, ToolHooks: ToolHooks{BeforeTool: func(ctx context.Context, call goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
					identity, e := api.ToolInvocation(ctx)
					if e != nil || identity.Caller != "tools" || identity.Key != "stable" || identity.Task == 0 || identity.ParentTask == 0 {
						t.Fatal(identity, e)
					}
					before.Add(1)
					if call.ID != "stable" || call.Name != "nested_hook" {
						t.Fatal(call)
					}
					if blocked {
						return &BeforeToolResult{Block: "blocked"}, nil
					}
					return &BeforeToolResult{Arguments: JSON{"n": 2}}, nil
				}, AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
					after.Add(1)
					if result.Content != "original" {
						t.Fatal(result)
					}
					result.Details["owned"] = false
					return &ToolResult{Content: "replacement", Diagnostics: []ToolDiagnostic{{Severity: "info", Message: "after hook"}}}, nil
				}}}
				if e := registry.Install(ext); e != nil {
					t.Fatal(e)
				}
				def := taskDefinition(t, "nested-hooks-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					result, e := r.CallTool(ctx, "stable", "nested_hook", JSON{"n": 1})
					if e != nil {
						return e
					}
					if blocked {
						if !result.IsError || result.Content != "Tool blocked" {
							t.Fatal(result)
						}
					} else if result.Content != "replacement" || len(result.Diagnostics) != 1 {
						t.Fatal(result)
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry})
				record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
				want := int64(1)
				if blocked {
					want = 0
				}
				if record.State.Outcome.Status != "completed" || before.Load() != 1 || after.Load() != want || runs.Load() != want {
					t.Fatal(record, before.Load(), after.Load(), runs.Load())
				}
			})
		})
	}
}
