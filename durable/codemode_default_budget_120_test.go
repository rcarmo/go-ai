package durable

import (
	"context"
	"testing"
)

type budgetCaptureSandbox120 struct{ budget int }

func (s *budgetCaptureSandbox120) Start(_ context.Context, _ string, start CodemodeStart) (CodemodeExecution, error) {
	s.budget = start.InterruptBudget
	return largeOutputExecution120{full: "done"}, nil
}

func TestCodemode120DefaultAndExplicitZeroBudget(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		sandbox := &budgetCaptureSandbox120{}
		def := taskDefinition(t, "default-budget-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.RunCodemode(ctx, "default", "source", sandbox); err != nil {
				return err
			}
			if sandbox.budget != 100000 {
				t.Error("missing upstream default", sandbox.budget)
			}
			zero := 0
			if _, err := r.runCodemode(ctx, "disabled", "source", sandbox, nil, nil, nil, &zero); err != nil {
				return err
			}
			if sandbox.budget != 0 {
				t.Error("explicit zero not preserved", sandbox.budget)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
