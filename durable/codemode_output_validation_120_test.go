package durable

import (
	"context"
	"testing"
)

type invalidOutputSandbox120 struct{}

func (invalidOutputSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	return invalidOutputExecution120{}, nil
}

type invalidOutputExecution120 struct{}

func (invalidOutputExecution120) Next(context.Context) (CodemodeEvent, error) {
	return CodemodeEvent{Result: &CodemodeResult{Output: "long spill output", StoreWrites: JSON{"same": 1}, StoreDeletes: []string{"same"}}}, nil
}
func (invalidOutputExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (invalidOutputExecution120) Close() error                                    { return nil }
func TestCodemode120InvalidResultCannotWriteOutputFiles(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		writer := &outputWriter120{}
		registry := NewRegistry()
		def := taskDefinition(t, "invalid-output-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.RunCodemode(ctx, "run", "// @options: {\"max_output_tokens\":1}\ncode", invalidOutputSandbox120{}); err == nil {
				t.Error("invalid patch accepted")
			}
			writer.mu.Lock()
			writes := writer.count
			writer.mu.Unlock()
			if writes != 0 {
				t.Error("invalid result wrote files", writes)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
