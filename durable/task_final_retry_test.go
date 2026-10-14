package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipPinnedRejectedFinalizationRetriesOnNextCommit(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release, reported := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
		var parentRuns atomic.Int64
		child := taskDefinition(t, "task.final-retry.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
		})
		var childID ID
		parent := taskDefinition(t, "task.final-retry.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			parentRuns.Add(1)
			if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				childID, err = tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				return nil, err
			}); err != nil {
				return err
			}
			<-entered
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("held"), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(error) {
			select {
			case reported <- struct{}{}:
			default:
			}
		}}, parent, child)
		cleanupTaskGates(t, release)
		parentID := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		held := observeTaskState(t, h, parentID, "completing")
		var rejects atomic.Int64
		installTaskAppend(t, h, taskBackendCore(t, b.store), func(kind byte, _, _ uint64, payload []byte) error {
			if kind != 2 {
				return nil
			}
			var commit commitRecord
			if err := json.Unmarshal(payload, &commit); err != nil {
				return err
			}
			for _, write := range commit.Writes {
				if write.Task != nil && write.Task.ID == parentID && terminalStatus(write.Task.Status) && rejects.CompareAndSwap(0, 1) {
					return reject("final rejected once")
				}
			}
			return nil
		})
		releaseTaskGate(release)
		awaitTaskSignal(t, reported)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		reopened := reopenStoreAfterHarnessClose(t, b.store)
		state, err := reopened.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		record, err := CanonicalTask(state.Tasks[parentID], h.session.limits)
		if err != nil || record.State.Status != "completing" || record.State.Outcome.Result.Value != held.State.Outcome.Result.Value {
			t.Fatal("rejection lost hold", record, err)
		}
		//1.2.0: settle the adopted hold only through a fresh Session.
		next := taskTestHarness(t, reopened, parent, child)
		waitPublicTask(t, next, parentID)
		final := observeTaskState(t, next, parentID, "terminal")
		if final.State.Outcome.Status != "completed" || final.State.Outcome.Result.Value != "held" || parentRuns.Load() != 1 || rejects.Load() != 1 {
			t.Fatal(final, parentRuns.Load(), rejects.Load())
		}
		waitPublicTask(t, next, childID)
	})
}
