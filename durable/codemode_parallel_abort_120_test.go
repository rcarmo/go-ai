package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120ParallelCallerAbortJoinsChildren(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		sandbox, err := NewQuickJSSandbox(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer sandbox.Close(bg)
		sandbox.ParallelCalls = true
		entered := make(chan struct{}, 2)
		cancelled := make(chan struct{}, 2)
		release := make(chan struct{})
		defer close(release)
		var effects atomic.Int64
		registry := NewRegistry()
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			entered <- struct{}{}
			<-ctx.Done()
			cancelled <- struct{}{}
			<-release
			return ToolResult{}, ctx.Err()
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "parallel-abort-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, err := r.RunCodemode(ctx, "run", `await Promise.all([tools.codemode_leaf({n:1}),tools.codemode_leaf({n:2})]);store("unsafe",true);`, sandbox)
			if err != nil {
				return err
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
		for i := 0; i < 2; i++ {
			awaitTaskSignal(t, entered)
		}
		done := make(chan error, 1)
		go func() { _, err := h.AbortTask(bg, id); done <- err }()
		for i := 0; i < 2; i++ {
			awaitTaskSignal(t, cancelled)
		}
		record, _, err := h.Task(bg, id)
		if err != nil || record.State.Outcome != nil {
			t.Fatal("abort did not join children", record, err)
		}
		// Release cooperative host cleanup only after verifying the terminal fence.
		release <- struct{}{}
		release <- struct{}{}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("abort did not join")
		}
		record = waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "aborted" || effects.Load() != 2 {
			t.Fatal(record.State.Outcome, effects.Load())
		}
	})
}
