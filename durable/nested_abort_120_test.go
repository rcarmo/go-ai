package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestNestedTool120AbortJoinsHostAndStoresCallerResult(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var childID atomic.Uint64
		registry := NewRegistry()
		reg := wrapRegistration("nested_abort")
		reg.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			return ToolResult{}, ctx.Err()
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "nested-abort-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			handle, e := r.StartTool(ctx, "abort", "nested_abort", JSON{})
			if e != nil {
				return e
			}
			childID.Store(uint64(handle.ID()))
			result, e := handle.Wait(ctx)
			if e != nil {
				return e
			}
			if !result.IsError || result.Content != "aborted" {
				t.Fatal(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		parent := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, entered)
		aborted := make(chan error, 1)
		go func() { _, e := h.AbortTask(bg, ID(childID.Load())); aborted <- e }()
		awaitTaskSignal(t, cancelled)
		state, e := h.Snapshot(bg)
		if e != nil || terminalStatus(state.Tasks[ID(childID.Load())].Status) {
			t.Fatal("abort invented host return", e)
		}
		releaseTaskGate(release)
		if e := watchReceive(t, aborted); e != nil {
			t.Fatal(e)
		}
		record := waitPublicTask(t, h, parent)
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("outcome:%+v error:%+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
func TestNestedTool120OutcomeCallbackAndUsageSettleOnce(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var commits atomic.Int64
		registry := NewRegistry()
		reg := wrapRegistration("nested_usage")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			return ToolResult{Content: "result", Commit: func(*Tx) error { commits.Add(1); return nil }}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "nested-once-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			first, e := r.CallTool(ctx, "one", "nested_usage", JSON{})
			if e != nil {
				return e
			}
			again, e := r.CallTool(ctx, "one", "nested_usage", JSON{})
			if e != nil {
				return e
			}
			if first.Content != again.Content {
				t.Fatal(first, again)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		record, e := h.WaitForTask(ctx, id)
		if e != nil || record.State.Outcome.Status != "completed" || commits.Load() != 1 {
			t.Fatal(record, e, commits.Load())
		}
	})
}
