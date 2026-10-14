package durable

import (
	"context"
	"testing"
)

func TestNestedTool120SequentialModeDoesNotSerializeNestedCalls(t *testing.T) {
	taskLimitedBackends(t, 3, func(t *testing.T, store Storage) {
		entered := make(chan struct{}, 2)
		release := make(chan struct{})
		registry := NewRegistry()
		reg := wrapRegistration("sequential_nested")
		reg.ExecutionMode = "sequential"
		reg.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
			return ToolResult{Content: "done"}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "mode-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			first, e := r.StartTool(ctx, "first", "sequential_nested", JSON{})
			if e != nil {
				return e
			}
			second, e := r.StartTool(ctx, "second", "sequential_nested", JSON{})
			if e != nil {
				return e
			}
			if _, e = first.Wait(ctx); e != nil {
				return e
			}
			if _, e = second.Wait(ctx); e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, store, Options{Registry: registry})
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		watchReceive(t, entered)
		watchReceive(t, entered)
		releaseTaskGate(release)
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
func TestNestedTool120InvalidControlSuppressesCallbackAndHarnessErrorsOmitStructured(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		callbacks := 0
		reg := wrapRegistration("invalid_control_nested")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{Content: "text", Control: &ToolControl{AddTools: []string{"bad/name"}}, Commit: func(*Tx) error { callbacks++; return nil }}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "control-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, e := r.CallToolAuto(ctx, "invalid_control_nested", JSON{})
			if e != nil {
				return e
			}
			if !result.IsError || result.HasStructuredOutput || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "invalid_control" {
				t.Fatal(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" || callbacks != 0 {
			t.Fatal(record, callbacks)
		}
	})
}
