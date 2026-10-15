package durable

import (
	"context"
	"strings"
	"testing"
)

func TestCodemode120LargeOutputSpillsWithinUnchangedStorageLimits(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		full := strings.Repeat("large line\n", (MaxCodemodeFullOutputBytes/11)-1)
		writer := &outputWriter120{}
		registry := NewRegistry()
		sandbox := largeOutputSandbox120{full: full}
		def := taskDefinition(t, "large-output-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", `{"code":"large output","max_output_tokens":1000000}`, sandbox)
			if err != nil {
				return err
			}
			if result.FullOutputPath != "/owned/output.txt" || len(result.Output) >= len(full) || !strings.Contains(result.Output, "Full output:") {
				t.Error("large output not safely spilled", len(result.Output), result.FullOutputPath)
			}
			if len(result.Output) > r.harness.session.limits.MaxStringBytes {
				t.Error("inline exceeds storage limit")
			}
			if writer.writes["output.txt"] != full {
				t.Error("spill lost full text", len(writer.writes["output.txt"]), len(full))
			}
			if _, err := r.RunCodemode(ctx, "run", `{"code":"large output","max_output_tokens":1000000}`, sandbox); err != nil {
				return err
			}
			if writer.count != 1 {
				t.Error("settled run rewrote output", writer.count)
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

type largeOutputSandbox120 struct{ full string }

func (s largeOutputSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	return largeOutputExecution120(s), nil
}

type largeOutputExecution120 struct{ full string }

func (e largeOutputExecution120) Next(context.Context) (CodemodeEvent, error) {
	return CodemodeEvent{Result: &CodemodeResult{Output: e.full}}, nil
}
func (largeOutputExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (largeOutputExecution120) Close() error                                    { return nil }
