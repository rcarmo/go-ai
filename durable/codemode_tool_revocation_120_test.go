package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestCodemode120RemovedToolReplaysConfirmedAndFencesFreshCall(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		sandbox := &revokeSandbox120{sandbox: engine, ready: make(chan struct{})}
		registry := NewRegistry()
		var effects atomic.Int64
		leaf := wrapRegistration("leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "confirmed"}, nil
		}
		if err = registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "removed-tool", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "script", `const first=await tools.leaf({n:1});if(first!=="confirmed")throw Error("lost delivery");try{await tools.leaf({n:2});throw Error("fresh effect allowed")}catch(e){if(e.message==="fresh effect allowed")throw e;return "revoked";}`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" || result.Value != "revoked" {
				t.Errorf("revocation result %+v", result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		if effects.Load() != 1 {
			t.Fatal(effects.Load())
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		registry.Remove("leaf")
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("outcome=%+v error=%+v effects=%d starts=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), sandbox.starts.Load())
		}
	})
}
