package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

type wallSandbox120 struct{ clock *atomic.Int64 }

func (s wallSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	s.clock.Store(2500)
	return &wallExecution120{}, nil
}

type wallExecution120 struct{}

func (*wallExecution120) Next(context.Context) (CodemodeEvent, error) {
	return CodemodeEvent{Result: &CodemodeResult{WallMs: 999999, Output: "ok"}}, nil
}
func (*wallExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (*wallExecution120) Close() error                                    { return nil }
func TestCodemode120WallTimeHostOwnedAndSettledReuse(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock atomic.Int64
		clock.Store(1000)
		registry := NewRegistry()
		def := taskDefinition(t, "wall-time", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "script", "return 1", wallSandbox120{&clock})
			if err != nil {
				return err
			}
			if result.WallMs != 1500 {
				t.Error("sandbox timing trusted", result.WallMs)
			}
			clock.Store(9000)
			again, err := r.RunCodemode(ctx, "script", "return 1", wallSandbox120{&clock})
			if err != nil {
				return err
			}
			if again.WallMs != 1500 {
				t.Error("settled timing changed", again.WallMs)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Now: clock.Load})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
