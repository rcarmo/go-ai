package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNestedTool120AutoKeysReplayOrder(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		reg := wrapRegistration("auto_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "done"}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		entered, release := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var phases atomic.Int64
		var ids [2][2]ID
		def := taskDefinition(t, "auto-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			phase := phases.Add(1) - 1
			for i := 0; i < 2; i++ {
				handle, e := r.StartToolAuto(ctx, "auto_leaf", JSON{"index": i})
				if e != nil {
					return e
				}
				ids[phase][i] = handle.ID()
				if _, e = handle.Wait(ctx); e != nil {
					return e
				}
			}
			if phase == 0 {
				close(entered)
				<-ctx.Done()
				<-release
				return ctx.Err()
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
		done := make(chan error, 1)
		go func() { done <- h.Close(bg) }()
		<-h.life.Done()
		releaseTaskGate(release)
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, parent)
		if record.State.Outcome.Status != "completed" || ids[0] != ids[1] || ids[0][0] == ids[0][1] || effects.Load() != 2 {
			t.Fatalf("record:%+v error:%+v ids:%v effects:%d", record.State.Outcome, record.State.Outcome.Error, ids, effects.Load())
		}
	})
}
func TestNestedTool120ConcurrentAutoAdmissionUniqueAndKeyedCoalescence(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		reg := wrapRegistration("concurrent_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "done"}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "concurrent-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			const count = 4
			handles := make([]*NestedToolHandle, count)
			errs := make([]error, count)
			var wg sync.WaitGroup
			for i := range count {
				wg.Add(1)
				go func(i int) { defer wg.Done(); handles[i], errs[i] = r.StartToolAuto(ctx, "concurrent_leaf", JSON{}) }(i)
			}
			wg.Wait()
			seen := map[ID]bool{}
			for i, handle := range handles {
				if errs[i] != nil {
					return errs[i]
				}
				if seen[handle.ID()] {
					t.Fatal("automatic key collision")
				}
				seen[handle.ID()] = true
				if _, e := handle.Wait(ctx); e != nil {
					return e
				}
			}
			for i := range count {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					handles[i], errs[i] = r.StartTool(ctx, "shared", "concurrent_leaf", JSON{})
				}(i)
			}
			wg.Wait()
			for i, handle := range handles {
				if errs[i] != nil {
					return errs[i]
				}
				if handle.ID() != handles[0].ID() {
					t.Fatal("concurrent keyed duplicate")
				}
			}
			if _, e := handles[0].Wait(ctx); e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" || effects.Load() != 5 {
			t.Fatalf("%+v %+v effects:%d", record.State.Outcome, record.State.Outcome.Error, effects.Load())
		}
	})
}
