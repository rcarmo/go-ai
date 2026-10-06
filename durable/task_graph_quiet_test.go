package durable

import (
	"context"
	"strconv"
	"testing"
	"time"
)

func TestTaskGraphPinnedMemoOnlyCommitPublishesNoNodeRevision(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		runtime := make(chan *TaskRuntime, 1)
		definition := taskDefinition(t, "task.graph-quiet", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			runtime <- r
			close(entered)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("done"), nil })
		})
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		r := <-runtime
		graph, err := h.TaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		frames := make(chan TaskGraph, 8)
		watch, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		if err := watch.Start(func(_ context.Context, graph TaskGraph, _ []Operation) error { frames <- graph; return nil }); err != nil {
			t.Fatal(err)
		}
		if _, err := r.MemoCandidate(bg, "private", JSON{"payload": "not a node"}); err != nil {
			t.Fatal(err)
		}
		rebuilt, err := h.TaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		left, err := dtoObject(graph, h.session.limits)
		if err != nil {
			t.Fatal(err)
		}
		right, err := dtoObject(rebuilt, h.session.limits)
		if err != nil || !equalJSONValue(left, right) {
			t.Fatal("memo changed graph", rebuilt, err)
		}
		releaseTaskGate(release)
		waitPublicTask(t, h, id)
		// The terminal graph callback is a positive delivery barrier after the
		// preceding memo publication. There must be no earlier identical callback.
		select {
		case frame := <-frames:
			if _, exists := frame.Tasks[strconv.FormatUint(uint64(id), 10)]; exists {
				t.Fatal("memo published node revision", frame)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("terminal graph missing")
		}
	})
}
