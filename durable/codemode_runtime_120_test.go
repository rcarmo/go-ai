package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type serialSandbox120 struct {
	starts       atomic.Int64
	failDelivery bool
	seed         uint64
	clock        int64
	diverge      bool
}

func (s *serialSandbox120) Start(_ context.Context, _ string, start CodemodeStart) (CodemodeExecution, error) {
	if s.starts.Add(1) == 1 {
		s.seed = start.Seed
		s.clock = start.StartedAt
	} else if s.seed != start.Seed || s.clock != start.StartedAt {
		return nil, errors.New("start changed on replay")
	}
	return &serialExecution120{sandbox: s}, nil
}

type serialExecution120 struct {
	sandbox *serialSandbox120
	step    int
}

func (e *serialExecution120) Next(context.Context) (CodemodeEvent, error) {
	if e.step == 0 {
		name := "codemode_leaf"
		if e.sandbox.diverge {
			name = "other"
		}
		return CodemodeEvent{Call: &CodemodeCall{ID: 1, Name: name, Arguments: JSON{"n": 1}}}, nil
	}
	return CodemodeEvent{Result: &CodemodeResult{Output: "done", HasValue: true, Value: JSON{"result": "leaf"}}}, nil
}
func (e *serialExecution120) Deliver(_ context.Context, d CodemodeDelivery) error {
	if e.sandbox.failDelivery && e.sandbox.starts.Load() == 1 {
		return errors.New("simulated VM loss")
	}
	if d.Seq != 1 || !d.HasValue || d.Value != "leaf" {
		return errors.New("delivery changed")
	}
	e.step++
	return nil
}
func (e *serialExecution120) Close() error { return nil }
func TestCodemode120SerialJournalAcknowledgedBeforeVMReplay(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		reg := wrapRegistration("codemode_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		sandbox := &serialSandbox120{failDelivery: true}
		def := taskDefinition(t, "codemode-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, e := r.RunCodemode(ctx, "run", "await tools.codemode_leaf({n:1})", sandbox); e == nil {
				return reject("VM failure missing")
			}
			result, e := r.RunCodemode(ctx, "run", "await tools.codemode_leaf({n:1})", sandbox)
			if e != nil {
				return e
			}
			if result.Output != "done" {
				t.Fatal(result)
			}
			result.Value.(map[string]any)["result"] = "mutated"
			again, e := r.RunCodemode(ctx, "run", "await tools.codemode_leaf({n:1})", sandbox)
			if e != nil {
				return e
			}
			if again.Value.(map[string]any)["result"] != "leaf" {
				t.Fatal("codemode result alias")
			}
			if _, e = r.RunCodemode(ctx, "run", "changed source", sandbox); e == nil {
				t.Fatal("source replay mismatch")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("%+v %+v effects:%d starts:%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), sandbox.starts.Load())
		}
		state, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind || doc.Kind == codemodeJournalKind {
				if !doc.Retired {
					t.Fatal("codemode document not retired")
				}
			}
		}
	})
}
func TestCodemode120RejectsSandboxAuthorityDivergence(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		def := taskDefinition(t, "bad-sandbox-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, e := r.RunCodemode(ctx, "key", "source", &serialSandbox120{diverge: true}); e == nil {
				t.Fatal("sandbox widened authority")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
	})
}
