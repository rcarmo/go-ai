package durable

import (
	"context"
	"strings"
	"testing"
)

type identitySandbox120 struct {
	identity string
	sandbox  CodemodeSandbox
	starts   int
}

func (s *identitySandbox120) RuntimeIdentity() string { return s.identity }
func (s *identitySandbox120) Start(ctx context.Context, code string, start CodemodeStart) (CodemodeExecution, error) {
	s.starts++
	return s.sandbox.Start(ctx, code, start)
}
func TestCodemode120RuntimeIdentityFencesSettledReuse(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		first := &identitySandbox120{identity: "engine-one", sandbox: &storeSandbox120{value: 1}}
		changed := &identitySandbox120{identity: "engine-two", sandbox: &storeSandbox120{value: 2}}
		def := taskDefinition(t, "identity-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.RunCodemode(ctx, "run", "source", first); err != nil {
				return err
			}
			if _, err := r.RunCodemode(ctx, "run", "source", changed); err == nil || !strings.Contains(err.Error(), "identity") {
				t.Error("changed engine admitted", err)
			}
			if changed.starts != 0 {
				t.Error("changed VM started")
			}
			if _, err := r.RunCodemode(ctx, "run", "source", first); err != nil {
				return err
			}
			if first.starts != 1 {
				t.Error("settled matching run restarted")
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
