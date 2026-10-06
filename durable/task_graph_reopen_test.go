package durable

import (
	"context"
	"strconv"
	"testing"
)

func TestTaskGraphPinnedReopenPendingMarksAndReverseCreatedConversationOrder(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		definition := taskDefinition(t, "task.graph-reopen", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(entered)
			<-release
			return ctx.Err()
		})
		h := taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, release)
		owner := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		var low, high ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			low, err = tx.MintID()
			if err != nil {
				return err
			}
			high, err = tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.CreateConversation(Conversation{ID: high, Owner: owner}); err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: low, Owner: owner})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		// Seed a retained durable mark while scheduling is paused, so no abort
		// invocation can settle before closing this actual ordinary invocation.
		h.session.taskBookkeeping(func() { h.scheduler.enabled.Store(false) })
		_, err = h.session.Commit(bg, func(tx *Tx) error {
			task := markTask(tx.state.Tasks[owner])
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = taskTestHarness(t, reopenStoreAfterHarnessClose(t, b.store), definition)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		graph, err := h.TaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		node := graph.Tasks[strconv.FormatUint(uint64(owner), 10)]
		if node.State.Status != "pending" || !node.AbortRequested || len(node.Conversations) != 2 || node.Conversations[0] != low || node.Conversations[1] != high {
			t.Fatal("reopen graph", graph)
		}
		watch, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer watch.Stop()
		baseline := watch.Value()
		if !baseline.Tasks[strconv.FormatUint(uint64(owner), 10)].AbortRequested {
			t.Fatal(baseline)
		}
		after, err := h.Snapshot(bg)
		if err != nil || before.Seq != after.Seq || h.scheduler.enabled.Load() {
			t.Fatal("graph enabled paused scheduler", before.Seq, after.Seq, err)
		}
	})
}
