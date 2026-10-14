package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestCodemode120ExactOptionNumbersAndInvalidOutputBudget(t *testing.T) {
	for _, raw := range []string{"1.0", "1e0", "1E+0", "10e-1"} {
		source, err := ParseCodemodeSource("// @options: {\"timeout_ms\":" + raw + "}\ncode")
		if err != nil || source.TimeoutMs != 1 {
			t.Fatal(raw, source, err)
		}
	}
	for _, raw := range []string{"9007199254740991.1", "1e999999999", "true", "\"1\"", "0.0000000000000000001", "1e-401"} {
		if _, err := ParseCodemodeSource("// @options: {\"max_output_tokens\":" + raw + "}\ncode"); err == nil {
			t.Fatal("unsafe number accepted", raw)
		}
	}
	for _, tc := range []struct {
		tokens int
		want   string
	}{{0, ""}, {1, "Inva"}, {100, "Invalid sandbox output"}} {
		if got := boundCodemodeOutput(string([]byte{255}), tc.tokens); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}

type advanceClockSandbox120 struct{ clock *atomic.Int64 }

func (s advanceClockSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	return advanceClockExecution120(s), nil
}

type advanceClockExecution120 struct{ clock *atomic.Int64 }

func (e advanceClockExecution120) Next(context.Context) (CodemodeEvent, error) {
	e.clock.Add(300000)
	return CodemodeEvent{Result: &CodemodeResult{Output: "late", StoreWrites: JSON{"late": true}}}, nil
}
func (advanceClockExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (advanceClockExecution120) Close() error                                    { return nil }
func TestCodemode120ExpiredResultCannotSettleStore(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock atomic.Int64
		clock.Store(1000)
		registry := NewRegistry()
		def := taskDefinition(t, "late-codemode-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, err := r.RunCodemode(ctx, "run", "source", advanceClockSandbox120{&clock})
			if !errors.Is(err, context.DeadlineExceeded) {
				return reject("late result admitted")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Now: clock.Load})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind && doc.Value["result"] != nil {
				t.Fatal("late result settled")
			}
			if doc.Kind == "pi.codemode.store" && len(doc.Value["values"].(map[string]any)) != 0 {
				t.Fatal("late store writes adopted")
			}
		}
	})
}
