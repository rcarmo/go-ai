package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

type midflightSandbox120 struct {
	sandbox           *QuickJSSandbox
	starts            atomic.Int64
	ready             chan struct{}
	release           chan struct{}
	acknowledgedCrash bool
}

func (s *midflightSandbox120) RuntimeIdentity() string             { return s.sandbox.RuntimeIdentity() }
func (s *midflightSandbox120) SupportsParallelCalls() bool         { return true }
func (s *midflightSandbox120) SupportsDrainAcknowledgements() bool { return true }
func (s *midflightSandbox120) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	vm, err := s.sandbox.Start(ctx, code, start)
	if err != nil {
		return nil, err
	}
	return &midflightExecution120{CodemodeExecution: vm, first: s.starts.Add(1) == 1, sandbox: s}, nil
}

type midflightExecution120 struct {
	CodemodeExecution
	first   bool
	sandbox *midflightSandbox120
}

func (e *midflightExecution120) Next(ctx context.Context) (CodemodeEvent, error) {
	event, err := e.CodemodeExecution.Next(ctx)
	if err == nil && e.first && !e.sandbox.acknowledgedCrash && event.Drain != nil && event.Drain.DeliveryID == 2 {
		close(e.sandbox.ready)
		<-ctx.Done()
		return CodemodeEvent{}, ctx.Err()
	}
	return event, err
}
func (e *midflightExecution120) AcknowledgeDrain(ctx context.Context, drain CodemodeDrain) error {
	err := e.CodemodeExecution.(CodemodeDrainExecution).AcknowledgeDrain(ctx, drain)
	if err == nil && e.first && e.sandbox.acknowledgedCrash && drain.DeliveryID == 2 {
		close(e.sandbox.ready)
		<-ctx.Done()
		return ctx.Err()
	}
	if err == nil && !e.first && drain.DeliveryID == 2 {
		close(e.sandbox.release)
	}
	return err
}

func TestCodemode120ParallelMidflightReopen(t *testing.T) {
	for _, acknowledged := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-count-commit", true: "after-count-commit"}[acknowledged], func(t *testing.T) { testParallelMidflight120(t, acknowledged) })
	}
}
func testParallelMidflight120(t *testing.T, acknowledged bool) {
	backends(t, func(t *testing.T, b backend) {
		engine, err := NewQuickJSSandbox(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close(bg)
		engine.ParallelCalls = true
		sandbox := &midflightSandbox120{sandbox: engine, ready: make(chan struct{}), release: make(chan struct{}), acknowledgedCrash: acknowledged}
		registry := NewRegistry()
		entered := make(chan struct{}, 1)
		var slowEffects, fastEffects atomic.Int64
		slow := wrapRegistration("codemode_slow")
		slow.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-sandbox.release:
				slowEffects.Add(1)
				return ToolResult{Content: "slow"}, nil
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
		}
		fast := wrapRegistration("codemode_fast")
		fast.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
			select {
			case <-entered:
				fastEffects.Add(1)
				return ToolResult{Content: "fast"}, nil
			case <-ctx.Done():
				return ToolResult{}, ctx.Err()
			}
		}
		for _, reg := range []ToolRegistration{slow, fast} {
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
		}
		def := taskDefinition(t, "midflight-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `const seen=[];const a=tools.codemode_slow({}).then(()=>seen.push(1));const b=tools.codemode_fast({}).then(()=>seen.push(2));await Promise.all([a,b]);return seen;`, sandbox)
			if err != nil {
				return err
			}
			if result.Error != "" {
				return reject(result.Error)
			}
			if fmt.Sprint(result.Value) != "[2 1]" {
				t.Error("replay observation order", result.Value)
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
		awaitTaskSignal(t, sandbox.ready)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, doc := range state.Documents {
			if doc.Kind == codemodeJournalKind {
				var j codemodeJournalState
				if err := fromObject(doc.Value, &j, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if len(j.Issued) != 2 || len(j.Deliveries) != 1 || j.Deliveries[0].Call.ID != 2 || (j.Deliveries[0].IssuedAfter != nil) != acknowledged || j.FirstIssued == nil || *j.FirstIssued != 2 {
					t.Fatal("wrong crash prefix", j)
				}
				found = true
			}
		}
		if !found || fastEffects.Load() != 1 || slowEffects.Load() != 0 {
			t.Fatal("unexpected midflight effects", found, fastEffects.Load(), slowEffects.Load())
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || fastEffects.Load() != 1 || slowEffects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatal(record.State.Outcome, fastEffects.Load(), slowEffects.Load(), sandbox.starts.Load())
		}
	})
}
