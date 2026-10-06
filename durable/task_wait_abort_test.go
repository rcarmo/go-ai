package durable

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestTaskSchedulerPinnedWaitingAbortFaultsBeforeForeignDependencyEnds(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		dependency := taskDefinition(t, "task.wait-abort.foreign", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("foreign"), nil })
		})
		var foreign ID
		var noProgress, throwing, successful atomic.Int64
		makeWaiter := func(kind string, abort TaskPhase) *TaskDefinition {
			t.Helper()
			def, err := DefineTask(TaskDefinitionOptions{Kind: kind, Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil }, Phases: map[string]TaskPhase{"run": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "run"}, On: []ID{foreign}, Policy: "allSettled"}, nil
				})
			}}, Abort: abort})
			if err != nil {
				t.Fatal(err)
			}
			return def
		}
		lazy := makeWaiter("task.wait-abort.lazy", func(context.Context, TaskRecord, *TaskRuntime) error { noProgress.Add(1); return nil })
		throws := makeWaiter("task.wait-abort.throw", func(context.Context, TaskRecord, *TaskRuntime) error {
			throwing.Add(1)
			return errors.New("host abort failed")
		})
		completes := makeWaiter("task.wait-abort.complete", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			successful.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "local"}}, nil
			})
		})
		h := taskTestHarness(t, b.store, dependency, lazy, throws, completes)
		foreign = createPublicTask(t, h, dependency, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		awaited := []ID{
			createPublicTask(t, h, lazy, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}),
			createPublicTask(t, h, throws, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}),
			createPublicTask(t, h, completes, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}),
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for _, id := range awaited {
			for {
				record, ok, err := h.Task(deadline, id)
				if err != nil || !ok {
					t.Fatal(record, ok, err)
				}
				if record.State.Status == "waiting" {
					break
				}
				select {
				case <-deadline.Done():
					t.Fatal("actual waiting state missing", deadline.Err())
				default:
					runtime.Gosched()
				}
			}
		}
		for index, id := range awaited {
			if status, err := h.AbortTask(deadline, id); err != nil || status != "marked" {
				t.Fatal(status, err)
			}
			record, err := h.WaitForTask(deadline, id)
			if err != nil {
				t.Fatal(err)
			}
			if index == 2 {
				if record.State.Outcome.Status != "aborted" || record.State.Outcome.Reason != "local" {
					t.Fatal("successful abort waited foreign dependency", record)
				}
			} else if record.State.Outcome.Status != "faulted" || record.State.Outcome.Error == nil {
				t.Fatal("abort did not fault independently of foreign task", record)
			} else {
				want := "host abort failed"
				if index == 0 {
					want = fmt.Sprintf("Abort handler of task %d returned without a terminal outcome", id)
				}
				if record.State.Outcome.Error.Message != want {
					t.Fatal("reference abort fault message", record.State.Outcome.Error.Message, want)
				}
			}
		}
		if noProgress.Load() != 1 || throwing.Load() != 1 || successful.Load() != 1 {
			t.Fatal("abort callbacks", noProgress.Load(), throwing.Load())
		}
		foreignRecord, ok, err := h.Task(deadline, foreign)
		if err != nil || !ok || foreignRecord.State.Status != "running" || foreignRecord.AbortRequested {
			t.Fatal("foreign dependency altered by abort", foreignRecord, ok, err)
		}
		close(release)
		if record, err := h.WaitForTask(deadline, foreign); err != nil || record.State.Outcome.Status != "completed" {
			t.Fatal(record, err)
		}
	})
}
