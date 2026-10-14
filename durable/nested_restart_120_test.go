package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

func TestNestedTool120RestartKeyAndReplayPolicy(t *testing.T) {
	for _, safe := range []bool{false, true} {
		name := "unsafe"
		if safe {
			name = "safe"
		}
		t.Run(name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				entered, release := make(chan struct{}), make(chan struct{})
				cleanupTaskGates(t, release)
				var effects atomic.Int64
				var ids [2]ID
				var parentRuns atomic.Int64
				reg := wrapRegistration("nested_restart")
				reg.ReplaySafe = safe
				reg.Callers = []string{"tools"}
				reg.Execute = func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					if effects.Add(1) == 1 {
						if e := api.Output("persisted-prefix"); e != nil {
							return ToolResult{}, e
						}
						if e := api.Details(JSON{"kept": true}); e != nil {
							return ToolResult{}, e
						}
						if e := api.Diagnostic(ToolDiagnostic{Severity: "warn", Message: "prior attempt"}); e != nil {
							return ToolResult{}, e
						}
						close(entered)
						<-ctx.Done()
						<-release
						return ToolResult{}, ctx.Err()
					}
					return ToolResult{Content: "recovered"}, nil
				}
				def := taskDefinition(t, "nested-restart-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					index := parentRuns.Add(1) - 1
					handle, e := r.StartTool(ctx, "key", "nested_restart", JSON{})
					if e != nil {
						return e
					}
					if index < 2 {
						ids[index] = handle.ID()
					}
					result, e := handle.Wait(ctx)
					if e != nil {
						return e
					}
					if safe && result.Content != "recovered" {
						t.Fatal(result)
					}
					if safe && len(result.Diagnostics) != 0 {
						t.Fatal("safe replay retained previous attempt diagnostics", result)
					}
					if !safe && (!result.IsError || result.Content != "persisted-prefix" || !result.HasDetails || len(result.Diagnostics) != 2) {
						t.Fatal(result)
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				registry := NewRegistry()
				if e := registry.Register(reg); e != nil {
					t.Fatal(e)
				}
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if e := h.Resume(bg); e != nil {
					t.Fatal(e)
				}
				awaitTaskSignal(t, entered)
				state, e := h.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				native := state.Tasks[ids[0]].Execution.Native
				progress, ok := native.State.Checkpoint["progress"].(map[string]any)
				if !ok || progress["output"] != "persisted-prefix" {
					t.Fatal("progress not durable", native.State.Checkpoint)
				}
				done := make(chan error, 1)
				go func() { done <- h.Close(bg) }()
				<-h.life.Done()
				releaseTaskGate(release)
				if e := <-done; e != nil {
					t.Fatal(e)
				}
				next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
				record := waitPublicTask(t, next, id)
				if record.State.Outcome.Status != "completed" {
					t.Fatalf("outcome:%+v error:%+v", record.State.Outcome, record.State.Outcome.Error)
				}
				want := int64(1)
				if safe {
					want = 2
				}
				if effects.Load() != want || ids[0] != ids[1] || parentRuns.Load() != 2 {
					t.Fatal("key/replay effects", effects.Load(), ids, parentRuns.Load())
				}
			})
		})
	}
}
func TestNestedTool120InvalidArgumentsDoNotExecute(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		reg := wrapRegistration("nested_invalid")
		reg.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}`)
		var runs atomic.Int64
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) { runs.Add(1); return ToolResult{}, nil }
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "nested-invalid-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, e := r.CallTool(ctx, "bad", "nested_invalid", JSON{})
			if e != nil {
				return e
			}
			if !result.IsError {
				t.Fatal(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" || runs.Load() != 0 {
			t.Fatal(record, runs.Load())
		}
	})
}
