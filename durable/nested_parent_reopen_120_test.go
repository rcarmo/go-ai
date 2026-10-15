package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestNestedTool120ParentAttributionSurvivesActiveRestart(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		entered := make(chan struct{})
		var leafRuns, outerRuns atomic.Int64
		leaf := wrapRegistration("restart_leaf")
		leaf.ReplaySafe = true
		leaf.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			if leafRuns.Add(1) == 1 {
				close(entered)
				<-ctx.Done()
				return ToolResult{}, ctx.Err()
			}
			return ToolResult{Content: "leaf"}, nil
		}
		outer := wrapRegistration("restart_outer")
		outer.ReplaySafe = true
		outer.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			outerRuns.Add(1)
			child, err := api.CallTool(ctx, "child", "restart_leaf", JSON{})
			if err != nil {
				return ToolResult{}, err
			}
			return ToolResult{Content: child.Content, IsError: child.IsError}, nil
		}
		for _, reg := range []ToolRegistration{leaf, outer} {
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
		}
		def := taskDefinition(t, "parent-attribution-reopen", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.CallTool(ctx, "outer", "restart_outer", JSON{})
			if err != nil {
				return err
			}
			if result.IsError || result.Content != "leaf" {
				t.Error("recovered nested result", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		original, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var childID ID
		for _, task := range original.Tasks {
			if task.Kind != nestedToolKind {
				continue
			}
			record, err := CanonicalTask(task, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			input, err := nestedInput(record, h.session.limits)
			if err != nil {
				t.Fatal(err)
			}
			if input.Name == "restart_leaf" {
				childID = task.ID
				if input.ParentCallID != "outer" {
					t.Fatal("initial parent attribution", input)
				}
			}
		}
		if childID == 0 {
			t.Fatal("child absent")
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || leafRuns.Load() != 2 || outerRuns.Load() != 2 {
			t.Fatal(record.State.Outcome, leafRuns.Load(), outerRuns.Load())
		}
		recovered, err := next.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		child, ok := recovered.Tasks[childID]
		if !ok {
			t.Fatal("keyed child replaced")
		}
		owned, err := CanonicalTask(child, next.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		input, err := nestedInput(owned, next.session.limits)
		if err != nil || input.ParentCallID != "outer" {
			t.Fatal("recovered attribution", input, err)
		}
	})
}
