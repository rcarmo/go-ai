package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestTaskSchedulerPinnedAbortTerminalOutcomeWinsLateThrow(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var aborts atomic.Int64
		definition := taskDefinition(t, "task.abort-late-throw", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(entered)
			<-release
			return ctx.Err()
		})
		definition.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "kept_abort"}}, nil
			}); err != nil {
				return err
			}
			return errors.New("private throw after abort terminal")
		}
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		result := make(chan error, 1)
		go func() { _, err := h.AbortTask(bg, id); result <- err }()
		for {
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if taskAborted(state.Tasks[id]) {
				break
			}
		}
		close(release)
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "aborted" || record.State.Outcome.Reason != "kept_abort" || aborts.Load() != 1 {
			t.Fatal(record, aborts.Load())
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), definition)
		record = waitPublicTask(t, second, id)
		if record.State.Outcome.Status != "aborted" || record.State.Outcome.Reason != "kept_abort" || aborts.Load() != 1 {
			t.Fatal("abort late error replayed", record, aborts.Load())
		}
	})
}
