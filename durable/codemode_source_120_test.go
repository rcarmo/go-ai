package durable

import (
	"context"
	"errors"
	"testing"
)

func TestCodemode120SourceDirectiveAndUnicodeOutput(t *testing.T) {
	source, e := ParseCodemodeSource("// @options: {\"timeout_ms\":10,\"max_output_tokens\":0}\nreturn 1")
	if e != nil || source.TimeoutMs != 10 || source.MaxOutputTokens != 0 || source.Code != "\nreturn 1" {
		t.Fatal(source, e)
	}
	for _, value := range []string{"", "// @options: {}", "code\n// @options: {}", "// @options: {\"unknown\":1}\ncode", "// @options: {\"timeout_ms\":0}\ncode", "// @options: {\"timeout_ms\":2147483648}\ncode", "// @options: {\"max_output_tokens\":1.5}\ncode", "// @options: {\"max_output_tokens\":-1}\ncode"} {
		if _, e := ParseCodemodeSource(value); e == nil {
			t.Fatal("invalid directive accepted", value)
		}
	}
	if got := boundCodemodeOutput("😀😀😀", 1); got != "😀😀" {
		t.Fatal(got)
	}
	if boundCodemodeOutput("text", 0) != "" {
		t.Fatal("zero budget")
	}
}

type deadlineSandbox120 struct{}

func (deadlineSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	return deadlineExecution120{}, nil
}

type deadlineExecution120 struct{}

func (deadlineExecution120) Next(ctx context.Context) (CodemodeEvent, error) {
	<-ctx.Done()
	return CodemodeEvent{}, ctx.Err()
}
func (deadlineExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (deadlineExecution120) Close() error                                    { return nil }
func TestCodemode120DeadlineAndReplayDoesNotReset(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		def := taskDefinition(t, "deadline-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			source := "// @options: {\"timeout_ms\":5}\nawait something()"
			if _, e := r.RunCodemode(ctx, "run", source, deadlineSandbox120{}); !errors.Is(e, context.DeadlineExceeded) {
				return reject("deadline not enforced")
			}
			if _, e := r.RunCodemode(ctx, "run", source, deadlineSandbox120{}); !errors.Is(e, context.DeadlineExceeded) {
				return reject("replay reset deadline")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
