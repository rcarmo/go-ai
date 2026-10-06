package durable

import (
	"context"
	"testing"
	"time"
)

func TestTaskOwnershipAncestorCascadeIncludesAlreadyMarkedBackgroundSubtree(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		started := make(chan ID, 3)
		releaseBackground := make(chan struct{})
		var parentID, backgroundID, childID ID
		live := taskDefinition(t, "task.reference.marked.background", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			started <- r.TaskID()
			<-ctx.Done()
			return ctx.Err()
		})
		options := live.options
		options.Abort = func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			if task.ID == backgroundID {
				<-releaseBackground
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}
		var err error
		live, err = DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, live)
		cleanupTaskGates(t, releaseBackground)
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			parentID, err = tx.CreateTask(live, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return err
			}
			var conversation ID
			conversation, err = tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.CreateConversation(Conversation{ID: conversation, Owner: parentID}); err != nil {
				return err
			}
			backgroundID, err = tx.CreateTask(live, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
			if err != nil {
				return err
			}
			childID, err = tx.CreateTask(live, nil, TaskOptions{Conversation: conversation, Ownership: TaskOwnership{Kind: "task", Task: backgroundID}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("marked subtree host missing")
			}
		}
		// Mark the background owner first; its abort stays live while the ancestor
		// cascade runs. This differs from including all background work by option.
		if _, err := h.AbortTask(bg, backgroundID); err != nil {
			t.Fatal(err)
		}
		parentAbort := make(chan error, 1)
		go func() { _, err := h.AbortTask(bg, parentID); parentAbort <- err }()
		if child := waitPublicTask(t, h, childID); child.State.Outcome.Status != "aborted" {
			t.Fatal(child)
		}
		state, err := h.Snapshot(bg)
		if err != nil || !taskAborted(state.Tasks[parentID]) || !taskAborted(state.Tasks[backgroundID]) {
			t.Fatal("marks missing", err)
		}
		// Ordinary ownership joins stop at background edges even when marked.
		// The ancestor may settle; the independently marked background abort
		// must stay live and complete its own subtree, without replaying it.
		background, found, err := h.Task(bg, backgroundID)
		if err != nil || !found || background.State.Status == "terminal" {
			t.Fatal("background abort lost its invocation", background, err)
		}
		releaseTaskGate(releaseBackground)
		select {
		case err := <-parentAbort:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("ancestor abort did not join")
		}
		for _, id := range []ID{backgroundID, parentID} {
			if final := waitPublicTask(t, h, id); final.State.Outcome.Status != "aborted" {
				t.Fatal(final)
			}
		}
	})
}
