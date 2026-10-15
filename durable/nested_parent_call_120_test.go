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
			snapshot, err := r.harness.Snapshot(ctx)
			if err != nil {
				return err
			}
			checked := false
			for _, task := range snapshot.Tasks {
				if task.Kind != nestedToolKind {
					continue
				}
				record, err := CanonicalTask(task, r.harness.session.limits)
				if err != nil {
					return err
				}
				input, err := nestedInput(record, r.harness.session.limits)
				if err != nil {
					return err
				}
				if input.Name != "leaf" {
					continue
				}
				input.ParentCallID = "forged"
				value, err := dtoObject(input, r.harness.session.limits)
				if err != nil {
					return err
				}
				task.Execution.Native.Input = &TaskValue{Present: true, Value: value}
				if err := validateNestedReferences(snapshot, task, r.harness.session.limits); err == nil {
					t.Error("forged parent attribution accepted")
				}
				checked = true
			}
			if !checked {
				t.Error("leaf task absent")
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

func TestNestedTool120ModelParentCallAttribution(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		leaf := wrapRegistration("leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "leaf"}, nil }
		outer := wrapRegistration("outer")
		outer.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			return api.CallTool(ctx, "child", "leaf", JSON{})
		}
		var identity ToolInvocation
		extension := &Extension{Name: "model-parent", Tools: []ToolRegistration{leaf, outer}, ToolHooks: ToolHooks{BeforeTool: func(ctx context.Context, call goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
			if call.Name == "leaf" {
				var err error
				identity, err = api.ToolInvocation(ctx)
				return nil, err
			}
			return nil, nil
		}}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		requests := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			requests++
			ch := make(chan goai.Event, 1)
			if requests == 1 {
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "model-call-120", Name: "outer", Arguments: map[string]any{}}}}}
			} else {
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "run"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sub.Wait(bg); err != nil {
			t.Fatal(err)
		}
		if identity.ParentCallID != "model-call-120" || identity.CallID != "child" || identity.ParentTask == 0 {
			t.Fatal("model parent attribution lost", identity)
		}
	})
}
