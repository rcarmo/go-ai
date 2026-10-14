package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestNestedTool120DeepCancellationJoinsLeafBeforeParentSettlement(t *testing.T) {
	taskLimitedBackends(t, 3, func(t *testing.T, store Storage) {
		registry := NewRegistry()
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var leafReturns atomic.Int64
		leaf := wrapRegistration("abort_leaf")
		leaf.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			close(entered)
			<-ctx.Done()
			close(cancelled)
			<-release
			leafReturns.Add(1)
			return ToolResult{}, ctx.Err()
		}
		if e := registry.Register(leaf); e != nil {
			t.Fatal(e)
		}
		middle := wrapRegistration("abort_middle")
		middle.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
			return api.CallToolAuto(ctx, "abort_leaf", JSON{})
		}
		if e := registry.Register(middle); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "deep-abort-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, e := r.CallToolAuto(ctx, "abort_middle", JSON{})
			if e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, store, Options{Registry: registry})
		cleanupTaskGates(t, release)
		parent := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, entered)
		done := make(chan error, 1)
		go func() { _, e := h.AbortTask(bg, parent); done <- e }()
		awaitTaskSignal(t, cancelled)
		state, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		for _, task := range state.Tasks {
			if task.Kind == nestedToolKind && terminalStatus(task.Status) {
				t.Fatal("deep abort invented nested return", task)
			}
		}
		if terminalStatus(state.Tasks[parent].Status) {
			t.Fatal("parent settled before retained leaf returned")
		}
		releaseTaskGate(release)
		if e := watchReceive(t, done); e != nil {
			t.Fatal(e)
		}
		waitPublicTask(t, h, parent)
		state, e = h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		if state.Tasks[parent].Status != "aborted" || leafReturns.Load() != 1 {
			t.Fatal("parent/leaf settlement", state.Tasks[parent], leafReturns.Load())
		}
		for _, task := range state.Tasks {
			if task.Kind == nestedToolKind && !terminalStatus(task.Status) {
				t.Fatal("nested abort not settled", task)
			}
		}
		for _, doc := range state.Documents {
			if doc.Kind == nestedCallDocument && !doc.Retired {
				t.Fatal("deep result retained after caller terminal", doc)
			}
		}
	})
}
