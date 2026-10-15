package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type gatedSandbox120 struct {
	entered, release chan struct{}
	starts           atomic.Int64
}

func (s *gatedSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	s.starts.Add(1)
	return &gatedExecution120{s}, nil
}

type gatedExecution120 struct{ s *gatedSandbox120 }

func (e *gatedExecution120) Next(ctx context.Context) (CodemodeEvent, error) {
	close(e.s.entered)
	select {
	case <-e.s.release:
		return CodemodeEvent{Result: &CodemodeResult{Output: "complete"}}, nil
	case <-ctx.Done():
		return CodemodeEvent{}, ctx.Err()
	}
}
func (e *gatedExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (e *gatedExecution120) Close() error                                    { return nil }
func TestCodemode120ConcurrentKeyRejectsSecondDriver(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		sandbox := &gatedSandbox120{entered: make(chan struct{}), release: make(chan struct{})}
		def := taskDefinition(t, "concurrent-codemode-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			done := make(chan error, 1)
			go func() { _, e := r.RunCodemode(ctx, "run", "source", sandbox); done <- e }()
			awaitTaskSignal(t, sandbox.entered)
			if _, e := r.RunCodemode(ctx, "run", "source", sandbox); e == nil {
				t.Error("concurrent journal driver admitted")
			}
			close(sandbox.release)
			if e := watchReceive(t, done); e != nil {
				return e
			}
			if sandbox.starts.Load() != 1 {
				return errors.New("multiple VMs launched")
			}
			if _, e := r.RunCodemode(ctx, "run", "source", sandbox); e != nil {
				return e
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
func TestCodemode120OptionsDuplicateLastWinsAndTrailingReject(t *testing.T) {
	parsed, err := ParseCodemodeSource("// @options: {\"timeout_ms\":1,\"timeout_ms\":2}\ncode")
	if err != nil || parsed.TimeoutMs != 2 {
		t.Fatal(parsed, err)
	}
	for _, source := range []string{"// @options: {} {}\ncode", "// @options: null\ncode", "// @options: []\ncode"} {
		if _, e := ParseCodemodeSource(source); e == nil {
			t.Fatal("bad options accepted", source)
		}
	}
}
