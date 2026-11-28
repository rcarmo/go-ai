package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// cancellationMarkBarrierStorage implements the source test's public storage
// hold point: a task cancellation mark is visible to the scheduler's commit but
// is not delivered to SQLite until release is closed.
type cancellationMarkBarrierStorage struct {
	Storage
	target  atomic.Uint64
	held    chan struct{}
	release chan struct{}
	once    sync.Once
}

func (s *cancellationMarkBarrierStorage) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	selected := false
	for _, batch := range batches {
		for _, write := range batch {
			if write.Type != "task" {
				continue
			}
			record, ok := recordValue[TaskRecord](write.Value)
			if ok && record.ID == ID(s.target.Load()) && record.AbortRequested {
				selected = true
			}
		}
	}
	if selected {
		s.once.Do(func() { close(s.held) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return CommitResult{}, ctx.Err()
		}
	}
	return s.Storage.Commit(ctx, batches, receive)
}

// TestTaskCancellationBarriersPreventUncommittedCascadePhaseAdvance translates
// the first assertion in pi v1.7.0
// packages/durable/test/harness-cancellation-barrier.test.ts. Public SQLite
// remains the persistence authority; the decorator holds only the selected
// cancellation mark at the source Storage.Commit boundary.
func TestTaskCancellationBarriersPreventUncommittedCascadePhaseAdvance(t *testing.T) {
	sqliteStore(t, func(t *testing.T, fixture sqliteFixture) {
		entered, advance := make(chan struct{}), make(chan struct{})
		var ranNext atomic.Bool
		parentDefinition := taskDefinition(t, "task.cancel-barrier.parent", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			<-ctx.Done()
			return ctx.Err()
		})
		childDefinition, err := DefineTask(TaskDefinitionOptions{
			Kind:    "task.cancel-barrier.child",
			Version: 1,
			Initial: func(any) (JSON, error) { return JSON{"phase": "first"}, nil },
			Phases: map[string]TaskPhase{
				"first": func(ctx context.Context, _ TaskRecord, runtime *TaskRuntime) error {
					close(entered)
					select {
					case <-advance:
					case <-ctx.Done():
						return ctx.Err()
					}
					return runtime.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "running", Checkpoint: JSON{"phase": "next"}}, nil
					})
				},
				"next": func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
					ranNext.Store(true)
					<-ctx.Done()
					return ctx.Err()
				},
			},
			Abort: func(ctx context.Context, _ TaskRecord, runtime *TaskRuntime) error {
				return runtime.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
				})
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		store := &cancellationMarkBarrierStorage{
			Storage: fixture.store,
			held:    make(chan struct{}),
			release: make(chan struct{}),
		}
		cleanupTaskGates(t, advance, store.release)
		harness := taskTestHarness(t, store, parentDefinition, childDefinition)
		parent := createPublicTask(t, harness, parentDefinition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		child := createPublicTask(t, harness, childDefinition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
		store.target.Store(uint64(child))
		if err := harness.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		caller, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		aborting := make(chan error, 1)
		go func() {
			_, err := harness.AbortTask(caller, parent)
			aborting <- err
		}()
		awaitTaskSignal(t, store.held)
		releaseTaskGate(advance)
		// Keep the child eligible to request its next phase while the cascade mark
		// is still held. No phase may start from that uncommitted request.
		time.Sleep(25 * time.Millisecond)
		if ranNext.Load() {
			t.Fatal("next phase ran while the child cancellation mark was uncommitted")
		}
		releaseTaskGate(store.release)
		select {
		case err := <-aborting:
			if err != nil {
				t.Fatal(err)
			}
		case <-caller.Done():
			t.Fatal(caller.Err())
		}
		record := waitPublicTask(t, harness, child)
		if record.State.Outcome == nil || record.State.Outcome.Status != "aborted" || ranNext.Load() {
			t.Fatal("cancelled child advanced", record, ranNext.Load())
		}
	})
}

// TestFailFastCancellationSupersedesRestartOnlyDescendantMarks translates the
// second assertion in the pinned cancellation-barrier original. A restart-
// abandoned owner is already waiting failFast on one failed child and one live
// child. The surviving child's cancellation is an explicit sibling selection,
// so a missing definition must be orphaned rather than retained as restart work.
func TestFailFastCancellationSupersedesRestartOnlyDescendantMarks(t *testing.T) {
	sqliteStore(t, func(t *testing.T, fixture sqliteFixture) {
		parentDefinition := taskDefinition(t, "task.cancel-barrier.failfast-parent", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			<-ctx.Done()
			return ctx.Err()
		})
		failedDefinition := taskDefinition(t, "task.cancel-barrier.failed-child", func(ctx context.Context, _ TaskRecord, runtime *TaskRuntime) error {
			return runtime.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("failed-child"), nil })
		})
		liveDefinition := taskDefinition(t, "task.cancel-barrier.live-child", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			<-ctx.Done()
			return ctx.Err()
		})
		first := taskTestHarness(t, fixture.store, parentDefinition, failedDefinition, liveDefinition)
		parent := createPublicTask(t, first, parentDefinition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		failed := createPublicTask(t, first, failedDefinition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
		live := createPublicTask(t, first, liveDefinition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
		_, err := first.commitTasks(bg, 1, func(tx *Tx) error {
			owner := markTask(tx.state.Tasks[parent])
			owner.AbortReason = "restart"
			owner.Status = "waiting"
			owner.Execution.Native.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{failed, live}, Policy: "failFast"}
			failedRecord := tx.state.Tasks[failed]
			failedRecord.Status = "failed"
			failedRecord.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "failed"}}}
			if err := tx.stage(Write{Op: "put-task", Task: &owner}); err != nil {
				return err
			}
			return tx.stage(Write{Op: "put-task", Task: &failedRecord})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}

		second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, fixture.store), parentDefinition)
		if err := second.Resume(bg); err != nil {
			t.Fatal(err)
		}
		caller, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		record, err := second.WaitForTask(caller, live)
		if err != nil {
			t.Fatal(err)
		}
		if record.State.Outcome == nil || record.State.Outcome.Status != "orphaned" || record.State.Outcome.Reason != "missing_task" || record.AbortReason != "" {
			t.Fatal("failFast request did not supersede restart mark", record)
		}
	})
}

// TestRestartMarkOnHeldOutcomeDrainsOwnedWorkAndKeepsOutcome translates the
// fourth assertion in the pinned cancellation-barrier original. The marked
// completing record is authoritative; restart cancellation drains its child
// without replacing the already held completion outcome.
func TestRestartMarkOnHeldOutcomeDrainsOwnedWorkAndKeepsOutcome(t *testing.T) {
	sqliteStore(t, func(t *testing.T, fixture sqliteFixture) {
		definition := taskDefinition(t, "task.cancel-barrier.held", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			<-ctx.Done()
			return ctx.Err()
		})
		first := taskTestHarness(t, fixture.store, definition)
		holder := createPublicTask(t, first, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, AbandonOnRestart: true})
		child := createPublicTask(t, first, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: holder}})
		_, err := first.commitTasks(bg, 1, func(tx *Tx) error {
			record := tx.state.Tasks[holder]
			record.Status = "completing"
			record.Execution.Native.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: nil}}}
			return tx.stage(Write{Op: "put-task", Task: &record})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}

		second := taskTestHarness(t, reopenStoreAfterHarnessClose(t, fixture.store), definition)
		if err := second.Resume(bg); err != nil {
			t.Fatal(err)
		}
		childRecord := waitPublicTask(t, second, child)
		if childRecord.State.Outcome == nil || childRecord.State.Outcome.Status != "aborted" {
			t.Fatal("owned work did not drain", childRecord)
		}
		holderRecord := waitPublicTask(t, second, holder)
		if holderRecord.AbortReason != "restart" || holderRecord.State.Outcome == nil || holderRecord.State.Outcome.Status != "completed" || holderRecord.State.Outcome.Result == nil || holderRecord.State.Outcome.Result.Value != nil {
			t.Fatal("restart mark replaced held outcome", holderRecord)
		}
	})
}
