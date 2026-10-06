package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestTaskOwnershipPinnedWaitingMissingDefinitionMigratesOnRegistration(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release, committed := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var dependencyID ID
		var migrations, runs atomic.Int64
		dependency := taskDefinition(t, "task.wait-migration.dependency", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("settled"), nil })
		})
		waiter, err := DefineTask(TaskDefinitionOptions{Kind: "task.wait-migration.waiter", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "wait"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
			"wait": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{dependencyID}, Policy: "allSettled"}, nil
				})
				close(committed)
				return err
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				runs.Add(1)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("v1"), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, dependency)
		cleanupTaskGates(t, release)
		dispose, err := h.options.Registry.RegisterTask(waiter)
		if err != nil {
			t.Fatal(err)
		}
		dependencyID = createPublicTask(t, h, dependency, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		id := createPublicTask(t, h, waiter, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		awaitTaskSignal(t, committed)
		observeTaskState(t, h, id, "waiting")
		dispose()
		releaseTaskGate(release)
		waitPublicTask(t, h, dependencyID)
		view, err := h.Inspect(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, task := range view.Tasks {
			if task.Record.ID == id {
				found = task.Kind == "blocked" && task.Reason == "missing_task"
			}
		}
		if !found {
			t.Fatal("waiting task not blocked", view)
		}
		stored, ok, err := h.Task(bg, id)
		if err != nil || !ok || stored.State.Status != "waiting" || stored.Version != 1 {
			t.Fatal(stored, err)
		}
		options := waiter.options
		options.Version = 2
		options.Migrate = func(input any, checkpoint JSON, _ uint64) (any, JSON, error) {
			migrations.Add(1)
			return input, checkpoint, nil
		}
		options.Phases = map[string]TaskPhase{"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runs.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("v2"), nil })
		}}
		newer, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.options.Registry.RegisterTask(newer); err != nil {
			t.Fatal(err)
		}
		// Observe without Wait/Resume: registry publication must wake scheduling.
		final := observeTaskState(t, h, id, "terminal")
		if final.Version != 2 || final.State.Outcome.Result.Value != "v2" || migrations.Load() != 1 || runs.Load() != 1 {
			t.Fatal(final, migrations.Load(), runs.Load())
		}
	})
}
