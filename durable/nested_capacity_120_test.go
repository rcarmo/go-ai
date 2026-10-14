package durable

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestNestedTool120BoundCapacityRejectsJoinWithoutDispatch(t *testing.T) {
	taskLimitedBackends(t, 1, func(t *testing.T, store Storage) {
		registry := NewRegistry()
		var effects atomic.Int64
		reg := wrapRegistration("capacity_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "capacity-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			handle, e := r.StartTool(ctx, "leaf", "capacity_leaf", JSON{})
			if e != nil {
				return e
			}
			_, e = handle.Wait(ctx)
			var yield *InvocationWaitRequiresYield
			if !errors.As(e, &yield) {
				return fmt.Errorf("nested join did not require yield: %w", e)
			}
			if effects.Load() != 0 {
				t.Fatal("capacity overflow dispatched")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		var record TaskRecord
		for {
			value, _, e := h.Task(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			record = value
			if record.State.Status == "terminal" {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}

		if record.State.Outcome.Status != "completed" || effects.Load() != 1 {
			t.Fatalf("outcome:%+v err:%+v effects:%d", record.State.Outcome, record.State.Outcome.Error, effects.Load())
		}
	})
}
func TestNestedTool120DeepCallsUseBoundedPermitsAndStableKeys(t *testing.T) {
	taskLimitedBackends(t, 3, func(t *testing.T, store Storage) {
		registry := NewRegistry()
		var leaves atomic.Int64
		leaf := wrapRegistration("deep_leaf")
		leaf.Callers = []string{"tools"}
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			leaves.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if e := registry.Register(leaf); e != nil {
			t.Fatal(e)
		}
		middle := wrapRegistration("deep_middle")
		middle.Callers = []string{"tools"}
		middle.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			first, e := api.CallTool(ctx, "leaf", "deep_leaf", JSON{})
			if e != nil {
				return ToolResult{}, e
			}
			again, e := api.CallTool(ctx, "leaf", "deep_leaf", JSON{})
			if e != nil {
				return ToolResult{}, e
			}
			if first.Content != again.Content {
				t.Fatal(first, again)
			}
			return ToolResult{Content: "middle:" + first.Content}, nil
		}
		if e := registry.Register(middle); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "deep-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, e := r.CallTool(ctx, "middle", "deep_middle", JSON{})
			if e != nil {
				return e
			}
			if result.Content != "middle:leaf" {
				t.Fatal(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" || leaves.Load() != 1 {
			t.Fatalf("%+v %+v leaves:%d", record.State.Outcome, record.State.Outcome.Error, leaves.Load())
		}
	})
}
func TestNestedTool120ExplicitKeyContract(t *testing.T) {
	for _, key := range []string{"", "__proto__", "a/b", "1", "123"} {
		if validNestedKey(key) {
			t.Fatal("invalid key accepted", key)
		}
	}
	for _, key := range []string{"stable", "0", "01", "-1", "a.b"} {
		if !validNestedKey(key) {
			t.Fatal("valid key rejected", key)
		}
	}
}
