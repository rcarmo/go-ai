package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

// Simulate replay-visible VM drift while retaining the pinned runtime identity.
// Source input remains unchanged, so checks must compare the issued call stream.
type driftSandbox120 struct {
	*restartQuickJS120
	replayCode string
}

func (s *driftSandbox120) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	if s.starts.Load() > 0 {
		code = s.replayCode
	}
	return s.restartQuickJS120.Start(ctx, code, start)
}
func TestCodemode120ReopenRejectsCallStreamDivergenceBeforeEffects(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	for _, tc := range []struct{ name, code, want string }{
		{"arguments", `await tools.leaf({n:99});return "drift";`, "sandbox call replay divergence"},
		{"fewer-calls", `await tools.leaf({n:1});return "drift";`, "codemode drain replay count divergence"},
		{"early-end", `return "drift";`, "codemode drain replay count divergence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				sandbox := &driftSandbox120{restartQuickJS120: &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}, replayCode: tc.code}
				registry := NewRegistry()
				var effects atomic.Int64
				leaf := wrapRegistration("leaf")
				leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					effects.Add(1)
					return ToolResult{Content: "ok"}, nil
				}
				if err = registry.Register(leaf); err != nil {
					t.Fatal(err)
				}
				def := taskDefinition(t, "replay-drift", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					_, err := r.RunCodemode(ctx, "script", `await tools.leaf({n:1});await tools.leaf({n:2});return "original";`, sandbox)
					if ctx.Err() != nil {
						return ctx.Err()
					}
					if err == nil || !strings.Contains(err.Error(), tc.want) {
						t.Errorf("drift not rejected: %v", err)
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
				if effects.Load() != 2 {
					t.Fatal("initial effects", effects.Load())
				}
				if err = h.Close(bg); err != nil {
					t.Fatal(err)
				}
				next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
				record := waitPublicTask(t, next, id)
				if record.State.Outcome.Status != "completed" || effects.Load() != 2 || sandbox.starts.Load() != 2 {
					t.Fatalf("outcome=%+v error=%+v effects=%d starts=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), sandbox.starts.Load())
				}
			})
		})
	}
}
