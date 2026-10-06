package durable

import (
	"context"
	"errors"
	"testing"
)

func TestInvocationConversationAbortBackgroundOptionsUsesBoundAuthority(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ownerEntered, releaseOwner, targetEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var runtime *TaskRuntime
		owner := taskDefinition(t, "task.bound.abort.owner", func(_ context.Context, _ TaskRecord, r *TaskRuntime) error {
			runtime = r
			close(ownerEntered)
			<-releaseOwner
			return nil
		})
		target := taskDefinition(t, "task.bound.abort.target", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(targetEntered)
			<-ctx.Done()
			return ctx.Err()
		})
		h := taskTestHarness(t, b.store, owner, target)
		cleanupTaskGates(t, releaseOwner)
		conversation, err := h.CreateConversation(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		var background ID
		if _, err := conversation.Commit(bg, func(tx *Tx) error {
			var err error
			background, err = tx.CreateTask(target, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		createPublicTask(t, h, owner, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, ownerEntered)
		awaitTaskSignal(t, targetEntered)
		bound, err := runtime.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		if err := bound.Abort(bg); err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil || taskAborted(state.Tasks[background]) {
			t.Fatal("default crossed background boundary", err)
		}
		if err := bound.AbortWithOptions(bg, ConversationAbortOptions{Background: true}); err != nil {
			t.Fatal(err)
		}
		result := waitPublicTask(t, h, background)
		if result.State.Outcome.Status != "aborted" {
			t.Fatal("bound background abort lost option", result)
		}
		if err := runtime.Commit(bg, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("owner"), nil }); err != nil {
			t.Fatal(err)
		}
		if err := bound.AbortWithOptions(bg, ConversationAbortOptions{Background: true}); !errors.Is(err, ErrSealed) {
			t.Fatal("ended bound background abort retained authority", err)
		}
		releaseTaskGate(releaseOwner)
	})
}
