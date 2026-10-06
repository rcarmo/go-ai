package durable

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// The reference seeds pre-crash records directly. These fixtures do the same
// through validated native storage transactions, then reopen paused.
func seedNativeRecoveryState(tx *Tx, id ID, state TaskState, marked bool) error {
	task, err := copyTask(tx.state.Tasks[id], tx.limits)
	if err != nil {
		return err
	}
	native := task.Execution.Native
	native.State = state
	native.AbortRequested = marked
	if state.Status == "terminal" || state.Status == "completing" {
		native.Memos = nil
	}
	task.Status = state.Status
	if state.Status == "completing" {
		task.Status = "completing"
	}
	if state.Status == "terminal" {
		task.Status = outcomeRawStatus(state.Outcome)
	}
	return tx.stage(Write{Op: "put-task", Task: &task})
}

func TestTaskRecoveryPinnedStructuredInterruptedTransitions(t *testing.T) {
	for _, variant := range []string{"all-settled", "fail-fast", "held-drained", "held-live", "abort-cascade"} {
		t.Run(variant, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var parentRuns, parentAborts, childRuns, childAborts atomic.Int64
				var children []ID
				release, started := make(chan struct{}), make(chan struct{})
				child := taskDefinition(t, "task.seed.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childRuns.Add(1)
					close(started)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("paid"), nil })
				})
				child.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					childAborts.Add(1)
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				parent := taskDefinition(t, "task.seed.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					parentRuns.Add(1)
					outcomes, err := r.Outcomes(ctx, children)
					if err != nil {
						return err
					}
					if len(outcomes) != 2 {
						return fmt.Errorf("missing ordered child outcomes: %v", outcomes)
					}
					expected := "completed"
					if variant == "fail-fast" {
						expected = "failed"
					}
					if outcomes[0].Status != expected || outcomes[1].Status != map[bool]string{true: "aborted", false: "completed"}[variant == "fail-fast"] {
						return fmt.Errorf("unordered outcomes: %v", outcomes)
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("joined"), nil })
				})
				parent.options.Abort = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					parentAborts.Add(1)
					if childAborts.Load() != 1 {
						return fmt.Errorf("parent aborted before child: %d", childAborts.Load())
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				}
				h := taskTestHarness(t, b.store, parent, child)
				cleanupTaskGates(t, release)
				var owner ID
				_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
					var err error
					owner, err = tx.CreateTask(parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
					if err != nil {
						return err
					}
					for i := 0; i < 2; i++ {
						id, err := tx.CreateTask(child, i, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: owner}})
						if err != nil {
							return err
						}
						children = append(children, id)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = h.session.Commit(bg, func(tx *Tx) error {
					for i, id := range children {
						state := *taskDone("already-paid")
						if variant == "fail-fast" {
							if i == 0 {
								state = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "declined"}}}
							} else {
								state = TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}}
							}
						}
						if variant == "held-live" && i == 1 || variant == "abort-cascade" && i == 1 {
							state = TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}}
						}
						if err := seedNativeRecoveryState(tx, id, state, false); err != nil {
							return err
						}
					}
					state := TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: children, Policy: "allSettled"}
					if variant == "fail-fast" {
						state.Policy = "failFast"
					}
					if variant == "held-drained" || variant == "held-live" {
						state = *taskDone("held")
						state.Status = "completing"
					}
					return seedNativeRecoveryState(tx, owner, state, variant == "abort-cascade")
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				h = taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), parent, child)
				if parentRuns.Load() != 0 || childRuns.Load() != 0 || childAborts.Load() != 0 {
					t.Fatal("paused open ran effects")
				}
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				if variant == "held-live" {
					awaitTaskSignal(t, started)
					record, ok, err := h.Task(bg, owner)
					if err != nil || !ok || record.State.Status != "completing" || record.State.Outcome.Result.Value != "held" {
						t.Fatal("lost held live work", record, err)
					}
					releaseTaskGate(release)
				}
				final := waitPublicTask(t, h, owner)
				want := "completed"
				if variant == "abort-cascade" {
					want = "aborted"
				}
				if final.State.Outcome.Status != want {
					t.Fatal(final)
				}
				switch variant {
				case "all-settled":
					if parentRuns.Load() != 1 || childRuns.Load() != 0 || childAborts.Load() != 0 {
						t.Fatal("replayed settled child", parentRuns.Load(), childRuns.Load(), childAborts.Load())
					}
				case "fail-fast":
					if parentRuns.Load() != 1 || childRuns.Load() != 0 || childAborts.Load() != 1 {
						t.Fatal("unmarked crash sibling ran", parentRuns.Load(), childRuns.Load(), childAborts.Load())
					}
				case "held-drained", "held-live":
					if final.State.Outcome.Result.Value != "held" || parentRuns.Load() != 0 || parentAborts.Load() != 0 {
						t.Fatal("held outcome replayed", final, parentRuns.Load(), parentAborts.Load())
					}
				case "abort-cascade":
					if parentRuns.Load() != 0 || childRuns.Load() != 0 || parentAborts.Load() != 1 || childAborts.Load() != 1 {
						t.Fatal("abort replay order", parentRuns.Load(), childRuns.Load(), parentAborts.Load(), childAborts.Load())
					}
				}
				if childRuns.Load() != map[bool]int64{true: 1, false: 0}[variant == "held-live"] {
					t.Fatal("child replay count", childRuns.Load())
				}
			})
		})
	}
}
