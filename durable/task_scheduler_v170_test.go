package durable

import (
	"context"
	"testing"
)

// TestTaskSchedulerV170CascadeReasonUsesNearestCancellingOwner ports the
// executable assertion in packages/durable/test/harness-scheduler-indexes.test.ts
// at pi v1.7.0 (fb0fe823539f6d04850f6e34748c01f01f212e1f). The original
// creates top -> middle -> leaf, marks top explicitly, and marks middle for
// restart; the leaf must inherit the middle's restart reason first.
func TestTaskSchedulerV170CascadeReasonUsesNearestCancellingOwner(t *testing.T) {
	sqliteStore(t, func(t *testing.T, fixture sqliteFixture) {
		definition := taskDefinition(t, "task.scheduler.v170.nearest-reason", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			<-ctx.Done()
			return ctx.Err()
		})
		harness := taskTestHarness(t, fixture.store, definition)
		top := createPublicTask(t, harness, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		middle := createPublicTask(t, harness, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: top}})
		leaf := createPublicTask(t, harness, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: middle}})

		_, err := harness.commitTasks(bg, 1, func(tx *Tx) error {
			topTask := markTask(tx.state.Tasks[top])
			if err := tx.stage(Write{Op: "put-task", Task: &topTask}); err != nil {
				return err
			}
			middleTask := markTask(tx.state.Tasks[middle])
			middleTask.AbortReason = "restart"
			return tx.stage(Write{Op: "put-task", Task: &middleTask})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := harness.scheduler.reconcile(); err != nil {
			t.Fatal(err)
		}
		state, err := harness.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if got := state.Tasks[leaf].AbortReason; got != "restart" {
			t.Fatalf("leaf abort reason = %q; want nearest cancelling owner's restart", got)
		}
	})
}
