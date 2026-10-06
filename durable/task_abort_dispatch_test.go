package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// Actual adopted reservation/dispatch seam; the storage-in-flight ordering
// variant remains separately mapped instead of assuming goroutine FIFO.
func TestTaskSchedulerMarkedAdoptedReservationStartsNoOrdinaryHandler(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var runs, aborts atomic.Int64
		definition := taskDefinition(t, "task.mark-before-dispatch", func(context.Context, TaskRecord, *TaskRuntime) error {
			runs.Add(1)
			return errors.New("ordinary handler must not start")
		})
		definition.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "marked_before_dispatch"}}, nil
			})
		}
		h := taskTestHarness(t, b.store, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		reserved, err := h.scheduler.reservePass(false)
		if err != nil || len(reserved) != 1 {
			t.Fatal(reserved, err)
		}
		r := reserved[0]
		if r.taskID != id {
			t.Fatal("wrong reservation")
		}
		// A registered real invocation owns its permit before code dispatch. Public
		// abort can now mark/cancel it and must join its actual return.
		caller, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		marked := make(chan error, 1)
		go func() { _, err := h.AbortTask(caller, id); marked <- err }()
		for {
			state, err := h.Snapshot(caller)
			if err != nil {
				go h.scheduler.run(r)
				t.Fatal(err)
			}
			if taskAborted(state.Tasks[id]) {
				break
			}
		}
		select {
		case err := <-marked:
			t.Fatal("abort failed to join undispatched permit", err)
		default:
		}
		go h.scheduler.run(r)
		if err := <-marked; err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "aborted" || runs.Load() != 0 || aborts.Load() != 1 {
			t.Fatal(record, runs.Load(), aborts.Load())
		}
	})
}

func TestTaskSchedulerPinnedAbortMarkWinsReturnedPhaseFault(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var aborts atomic.Int64
		definition := taskDefinition(t, "task.abort-wins-fault", func(context.Context, TaskRecord, *TaskRuntime) error {
			close(entered)
			<-release
			return errors.New("phase fault after durable abort")
		})
		definition.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			aborts.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "mark_wins"}}, nil
			})
		}
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		caller, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		marked := make(chan error, 1)
		go func() { _, err := h.AbortTask(caller, id); marked <- err }()
		for {
			state, err := h.Snapshot(caller)
			if err != nil {
				t.Fatal(err)
			}
			if taskAborted(state.Tasks[id]) {
				break
			}
		}
		close(release)
		if err := <-marked; err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "aborted" || record.State.Outcome.Reason != "mark_wins" || aborts.Load() != 1 {
			t.Fatal(record, aborts.Load())
		}
	})
}
