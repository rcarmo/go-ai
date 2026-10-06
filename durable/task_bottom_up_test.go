package durable

import (
	"context"
	"strings"
	"testing"
)

func TestTaskOwnershipPinnedBottomUpThreeLevelsAndAbortCannotWait(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var midID, leafID ID
		log := []string{}
		leaf := taskDefinition(t, "task.bottom.leaf", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(entered)
			<-release
			return ctx.Err()
		})
		leaf.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			log = append(log, "leaf")
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "leaf"}}, nil
			})
		}
		mid := taskDefinition(t, "task.bottom.mid", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				leafID, err = tx.CreateTask(leaf, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{leafID}, Policy: "allSettled"}, nil
			})
		})
		mid.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			child, ok, err := r.Task(ctx, leafID)
			if err != nil || !ok || child.State.Status != "terminal" {
				t.Error("mid aborted before leaf", child, err)
			}
			log = append(log, "mid")
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "mid"}}, nil
			})
		}
		top := taskDefinition(t, "task.bottom.top", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				midID, err = tx.CreateTask(mid, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				if err != nil {
					return nil, err
				}
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{midID}, Policy: "allSettled"}, nil
			})
		})
		top.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			child, ok, err := r.Task(ctx, midID)
			if err != nil || !ok || child.State.Status != "terminal" {
				t.Error("top aborted before mid", child, err)
			}
			log = append(log, "top")
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "top"}}, nil
			})
		}
		badEntered, badRelease := make(chan struct{}), make(chan struct{})
		var badAborts int
		bad := taskDefinition(t, "task.bottom.bad-abort", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(badEntered)
			<-badRelease
			return ctx.Err()
		})
		bad.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			badAborts++
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{}, Policy: "allSettled"}, nil
			})
		}
		h := taskTestHarness(t, b.store, leaf, mid, top, bad)
		cleanupTaskGates(t, release, badRelease)
		topID := createPublicTask(t, h, top, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		result := make(chan error, 1)
		go func() { _, err := h.AbortTask(bg, topID); result <- err }()
		for {
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if taskAborted(state.Tasks[leafID]) {
				break
			}
		}
		close(release)
		if err := <-result; err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h, topID)
		if record.State.Outcome.Status != "aborted" || strings.Join(log, ",") != "leaf,mid,top" {
			t.Fatal(record, log)
		}
		// A fresh abort handler returning waiting is faulted, never parked.
		badID := createPublicTask(t, h, bad, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		awaitTaskSignal(t, badEntered)
		abortResult := make(chan error, 1)
		go func() { _, err := h.AbortTask(bg, badID); abortResult <- err }()
		for {
			state, err := h.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			if taskAborted(state.Tasks[badID]) {
				break
			}
		}
		close(badRelease)
		if err := <-abortResult; err != nil {
			t.Fatal(err)
		}
		record = waitPublicTask(t, h, badID)
		if record.State.Outcome.Status != "faulted" || badAborts != 1 {
			t.Fatal("abort waiting accepted", record, badAborts)
		}
	})
}
