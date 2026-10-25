package durable

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func graphReason140(t *testing.T, frames <-chan TaskGraph, key, want string) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case graph := <-frames:
			if node, ok := graph.Tasks[key]; ok && node.Blocked == want {
				return
			}
		case <-timer.C:
			t.Fatalf("graph never reached blocked=%q", want)
		}
	}
}
func TestTaskGraph140BlockedMetadataWithoutEffectsOrCommits(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var calls atomic.Int64
		def := taskDefinition(t, "task.graph-block-140", func(context.Context, TaskRecord, *TaskRuntime) error { calls.Add(1); return nil })
		registry := NewRegistry()
		remove, err := registry.RegisterTask(def)
		if err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		key := strconv.FormatUint(uint64(id), 10)
		watch, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		frames := make(chan TaskGraph, 32)
		if err = watch.Start(func(ctx context.Context, g TaskGraph, _ []Operation) error {
			if _, err := h.TaskGraph(ctx); err != nil {
				return err
			}
			frames <- g
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		remove()
		graphReason140(t, frames, key, "missing_task")
		// Registry replacement alone changes the fit; migrations must not execute.
		options := def.options
		options.Version = 2
		newer, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = registry.RegisterTask(newer); err != nil {
			t.Fatal(err)
		}
		graphReason140(t, frames, key, "migration_failed")
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		graphReason140(t, frames, key, "")
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq || calls.Load() != 0 {
			t.Fatal("metadata ran effects or committed", err, calls.Load())
		}
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			task := tx.state.Tasks[id]
			task.Restarts = 5
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		graphReason140(t, frames, key, "restart_limit")
		parked, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		cap := uint64(6)
		if err = h.SetSettings(bg, HarnessSettings{RestartLimit: &cap}); err != nil {
			t.Fatal(err)
		}
		graphReason140(t, frames, key, "")
		raised, err := h.Snapshot(bg)
		if err != nil || raised.Seq != parked.Seq || calls.Load() != 0 {
			t.Fatal("raised cap dispatched paused task or committed", err, calls.Load())
		}
		graph, err := h.TaskGraph(bg)
		if err != nil || graph.Tasks[key].Blocked != "" {
			t.Fatal(graph, err)
		}
	})
}
func TestTaskGraph140MetadataWakeCoalescingAndSerialCallbacks(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		def := taskDefinition(t, "task.graph-coalesce-140", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		h := taskTestHarness(t, b.store, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		_, err := h.session.Commit(bg, func(tx *Tx) error {
			task := tx.state.Tasks[id]
			task.Restarts = 5
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		watch, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		entered, release := make(chan struct{}), make(chan struct{})
		cleanupTaskGates(t, release)
		var callbacks atomic.Int64
		frames := make(chan TaskGraph, 8)
		if err = watch.Start(func(_ context.Context, graph TaskGraph, _ []Operation) error {
			frames <- graph
			if callbacks.Add(1) == 1 {
				close(entered)
				<-release
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		cap := uint64(6)
		if err = h.SetSettings(bg, HarnessSettings{RestartLimit: &cap}); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		for n := 0; n < 100; n++ {
			h.session.refreshTaskGraphObservers()
		}
		watch.sub.mu.Lock()
		pending, bytes := len(watch.sub.pending), watch.sub.bytes
		watch.sub.mu.Unlock()
		if pending != 1 || bytes != 0 || callbacks.Load() != 1 {
			t.Fatal("unbounded/concurrent metadata delivery", pending, bytes, callbacks.Load())
		}
		// Subjects retain metadata only, never the task's checkpoint or input.
		if len(watch.subjects) != 1 {
			t.Fatal(watch.subjects)
		}
		writer, cancel := context.WithCancel(bg)
		_, err = h.session.Commit(writer, func(tx *Tx) error {
			task := tx.state.Tasks[id]
			task.Restarts = 6
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		cancel() // Already adopted: this must not terminate the graph watch.
		if err != nil {
			t.Fatal(err)
		}
		close(release)
		graphReason140(t, frames, strconv.FormatUint(uint64(id), 10), "restart_limit")
		select {
		case <-watch.Closed():
			t.Fatal("writer cancellation ended graph watch")
		default:
		}
	})
}
