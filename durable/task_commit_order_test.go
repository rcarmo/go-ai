package durable

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskSchedulerPinnedRuntimeCommitBeforeHostReturnWinsLateCommit(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, hostReturn, commitEntered, commitRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, hostReturn, commitRelease)
		captured := make(chan *TaskRuntime, 1)
		definition := taskDefinition(t, "task.commit.ordered", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			captured <- r
			close(entered)
			<-hostReturn
			return nil
		})
		h := taskTestHarness(t, b.store, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		owned := <-captured
		before, after := make(chan error, 1), make(chan error, 1)
		go func() {
			before <- owned.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) {
				close(commitEntered)
				<-commitRelease // Actual first commit owns the Session line.
				return taskDone("before"), nil
			})
		}()
		awaitTaskSignal(t, commitEntered)
		close(hostReturn)
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		// Phase-draining publication witnesses actual handler return before the
		// scheduler can acquire the line for its next step; no timer/FIFO premise.
		for {
			owned.phaseMu.Lock()
			draining := owned.phaseDraining
			owned.phaseMu.Unlock()
			if draining {
				break
			}
			select {
			case <-deadline.Done():
				t.Fatal("actual handler return missing", deadline.Err())
			default:
				runtime.Gosched()
			}
		}
		probe := &taskAdmissionContext{Context: deadline, queued: make(chan struct{})}
		var callbacks atomic.Int64
		go func() {
			after <- owned.Commit(probe, func(*Tx, TaskRecord) (*TaskState, error) { callbacks.Add(1); return taskDone("after"), nil })
		}()
		awaitTaskSignal(t, probe.queued)
		close(commitRelease)
		select {
		case err := <-before:
			if err != nil {
				t.Fatal("pre-return commit rejected", err)
			}
		case <-deadline.Done():
			t.Fatal("first commit did not finish", deadline.Err())
		}
		select {
		case err := <-after:
			if !errors.Is(err, ErrSealed) || callbacks.Load() != 0 {
				t.Fatal("post-return commit ran/replaced outcome", err, callbacks.Load())
			}
		case <-deadline.Done():
			t.Fatal("late commit did not reject", deadline.Err())
		}
		record, err := h.WaitForTask(deadline, id)
		if err != nil || record.State.Outcome.Status != "completed" || record.State.Outcome.Result.Value != "before" {
			t.Fatal("first outcome changed", record, err)
		}
		awaitTaskSignal(t, owned.done)
		if err := owned.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { callbacks.Add(1); return taskDone("ended"), nil }); !errors.Is(err, ErrSealed) || callbacks.Load() != 0 {
			t.Fatal("ended commit", err, callbacks.Load())
		}
	})
}
