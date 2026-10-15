package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120ParallelRuntimeCompletionOrder(t *testing.T) { testParallelRuntime120(t, false) }
func TestCodemode120ParallelRuntimeReopen(t *testing.T)          { testParallelRuntime120(t, true) }
func testParallelRuntime120(t *testing.T, reopen bool) {
	backends(t, func(t *testing.T, b backend) {
		sandbox, err := NewQuickJSSandbox(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer sandbox.Close(bg)
		sandbox.ParallelCalls = true
		wrapped := &restartQuickJS120{sandbox: sandbox, ready: make(chan struct{})}
		var runner CodemodeSandbox = sandbox
		if reopen {
			runner = wrapped
		}
		registry := NewRegistry()
		second := make(chan struct{})
		third := make(chan struct{})
		var effects atomic.Int64
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(ctx context.Context, args JSON, _ *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			if fmt.Sprint(args["n"]) == "1" {
				select {
				case <-second:
				case <-ctx.Done():
					return ToolResult{}, ctx.Err()
				}
				select {
				case <-third:
				case <-ctx.Done():
					return ToolResult{}, ctx.Err()
				}
			} else if fmt.Sprint(args["n"]) == "2" {
				close(second)
			} else {
				close(third)
			}
			return ToolResult{Content: "ok"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "parallel-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const seen=[];const a=tools.codemode_leaf({n:1}).then(()=>seen.push(1));const b=tools.codemode_leaf({n:2}).then(async()=>{seen.push(2);await tools.codemode_leaf({n:3});});await Promise.all([a,b]);return seen;`, runner)
			if err != nil {
				return err
			}
			if result.Error != "" {
				return reject(result.Error)
			}
			if !equalJSONValue(result.Value, []any{float64(2), float64(1)}) {
				t.Fatal("wrong observer order", result.Value)
			}
			snap, err := r.harness.Snapshot(ctx)
			if err != nil {
				return err
			}
			for _, doc := range snap.Documents {
				if doc.Owner == r.taskID && doc.Kind == codemodeJournalKind {
					var j codemodeJournalState
					if err := fromObject(doc.Value, &j, r.harness.session.limits); err != nil {
						return err
					}
					if len(j.Deliveries) != 3 || j.Deliveries[0].Call.ID != 2 || j.FirstIssued == nil || *j.FirstIssued != 2 {
						t.Fatal("wrong completion journal", j)
					}
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if reopen {
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, wrapped.ready)
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 3 {
			t.Fatal(record.State.Outcome, effects.Load())
		}
	})
}

func TestCodemode120ParallelEarlyCompletionJoinsCallbacks(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `tools.leaf({});return "finished";`, CodemodeStart{ParallelCalls: true, DrainAcknowledgements: true, Tools: []CodemodeToolDescription{{Name: "leaf"}}})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	event, err := vm.Next(ctx)
	if err != nil || event.Call == nil {
		t.Fatal(event, err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Drain == nil || event.Drain.Issued != 1 {
		t.Fatal(event, err)
	}
	if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
		t.Fatal(err)
	}
	event, err = vm.Next(ctx)
	if err != nil || event.Result == nil || event.Result.Error != "" {
		t.Fatal(event, err)
	}
}
