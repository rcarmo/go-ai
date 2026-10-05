package durable

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestTaskGraphCommittedWatchFamilyAndDetachedRebuild(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		releaseChild, releaseLate := make(chan struct{}), make(chan struct{})
		childStarted, lateStarted := make(chan struct{}), make(chan struct{})
		var first, late, owner ID
		child := taskDefinition(t, "task.graph.child", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if task.Input.Value == "late" {
				close(lateStarted)
				<-releaseLate
			} else {
				close(childStarted)
				<-releaseChild
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.graph.parent", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil }, Phases: map[string]TaskPhase{
			"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					for i := 0; i < 2; i++ {
						id, err := tx.MintID()
						if err != nil {
							return nil, err
						}
						if err = tx.CreateConversation(Conversation{ID: id, Owner: r.TaskID()}); err != nil {
							return nil, err
						}
					}
					return nil, nil
				}); err != nil {
					return err
				}
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					var err error
					first, err = tx.CreateTask(child, "first", TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{first}, Policy: "allSettled"}, err
				})
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					var err error
					late, err = tx.CreateTask(child, "late", TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish"}}, err
				})
			},
			"finish": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
			},
		}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) { t.Error("scheduler", err) }}, parent, child)
		cleanupTaskGates(t, releaseChild, releaseLate)
		watch, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		if len(watch.Value().Tasks) != 0 {
			t.Fatal("nonempty baseline")
		}
		frames := make(chan TaskGraph, 64)
		mirror := JSON{"tasks": JSON{}}
		if err := watch.Start(func(_ context.Context, graph TaskGraph, ops []Operation) error {
			applied, err := ApplyOperations(mirror, ops, h.session.limits)
			if err != nil {
				return err
			}
			value, err := dtoObject(graph, h.session.limits)
			if err != nil {
				return err
			}
			if !equalJSONValue(applied, value) {
				return errors.New("graph operations do not replay frame")
			}
			mirror = JSON(applied.(map[string]any))
			frames <- copyTaskGraph(graph)
			// Callback may reenter; neither mutation changes observer/storage state.
			if _, err := h.TaskGraph(bg); err != nil {
				return err
			}
			for key := range graph.Tasks {
				delete(graph.Tasks, key)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		owner = createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		key := strconv.FormatUint(uint64(owner), 10)
		await := func(test func(TaskGraph) bool) TaskGraph {
			t.Helper()
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			for {
				select {
				case graph := <-frames:
					if test(graph) {
						return graph
					}
				case <-ctx.Done():
					end, _ := watch.End()
					t.Fatal("graph frame missing", end)
					return TaskGraph{}
				}
			}
		}
		initial := await(func(g TaskGraph) bool { return g.Tasks[key].State.Status == "pending" })
		if initial.Tasks[key].State.Phase != "spawn" {
			t.Fatal(initial)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, childStarted)
		waiting := await(func(g TaskGraph) bool { return g.Tasks[key].State.Status == "waiting" })
		node := waiting.Tasks[key]
		if node.State.Policy != "allSettled" || len(node.State.On) != 1 || node.State.On[0] != first || len(node.Conversations) != 2 || node.Conversations[0] >= node.Conversations[1] {
			t.Fatal("waiting graph", node)
		}
		fresh, err := h.TaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		if len(fresh.Tasks[key].Conversations) != 2 {
			t.Fatal("rebuild", fresh)
		}
		detached := watch.Value()
		delete(detached.Tasks, key)
		if _, ok := watch.Value().Tasks[key]; !ok {
			t.Fatal("Value escaped")
		}
		releaseTaskGate(releaseChild)
		awaitTaskSignal(t, lateStarted)
		held := await(func(g TaskGraph) bool { return g.Tasks[key].State.Status == "completing" })
		if held.Tasks[key].State.Outcome != "completed" || held.Tasks[strconv.FormatUint(uint64(first), 10)].ID != 0 || len(held.Tasks[key].Conversations) != 2 {
			t.Fatal("held graph", held)
		}
		releaseTaskGate(releaseLate)
		waitPublicTask(t, h, owner)
		waitPublicTask(t, h, late)
		await(func(g TaskGraph) bool { return len(g.Tasks) == 0 })
		empty, err := h.TaskGraph(bg)
		if err != nil || len(empty.Tasks) != 0 {
			t.Fatal("terminal graph", empty, err)
		}
	})
}

func TestTaskGraphPausedCancellationAndClose(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		def := taskDefinition(t, "task.graph.paused", func(context.Context, TaskRecord, *TaskRuntime) error { t.Error("graph read ran host"); return nil })
		h := taskTestHarness(t, b.store, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		epoch := h.scheduler.epoch.Load()
		graph, err := h.TaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		node := graph.Tasks[strconv.FormatUint(uint64(id), 10)]
		if !node.Background || node.State.Status != "pending" || h.scheduler.enabled.Load() || h.scheduler.epoch.Load() != epoch {
			t.Fatal("graph read woke paused harness", graph)
		}
		ctx, cancel := context.WithCancel(bg)
		watch, err := h.WatchTaskGraph(ctx)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		awaitTaskSignal(t, watch.Closed())
		second, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, second.Closed())
		if _, err := h.TaskGraph(bg); !errors.Is(err, ErrClosed) {
			t.Fatal("closed graph read", err)
		}
		if _, err := h.WatchTaskGraph(bg); !errors.Is(err, ErrClosed) {
			t.Fatal("closed graph watch", err)
		}
	})
}
