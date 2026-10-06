package durable

import (
	"context"
	"testing"
	"time"
)

func TestTaskOwnershipPinnedLateOwnedConversationWorkExtendsCompletedHold(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		earlyEntered, earlyRelease, lateEntered, lateRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
		early := taskDefinition(t, "task.late-hold.early", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(earlyEntered)
			<-earlyRelease
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("early"), nil })
		})
		late := taskDefinition(t, "task.late-hold.late", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(lateEntered)
			<-lateRelease
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("late"), nil })
		})
		var owned ID
		parent := taskDefinition(t, "task.late-hold.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			conversation, err := r.CreateOwnedConversation(ctx, "owned", nil)
			if err != nil {
				return err
			}
			owned = conversation.ID()
			if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				_, err := tx.CreateTask(early, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
				return nil, err
			}); err != nil {
				return err
			}
			<-earlyEntered
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("parent"), nil })
		})
		h := taskTestHarness(t, b.store, parent, early, late)
		cleanupTaskGates(t, earlyRelease, lateRelease)
		// Owned-conversation acquisition inherits the real stored agent.
		if _, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}}); err != nil {
			t.Fatal(err)
		}
		parentID := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, earlyEntered)
		caller, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			state, err := h.Snapshot(caller)
			if err != nil {
				t.Fatal(err)
			}
			if state.Tasks[parentID].Status == "completing" {
				break
			}
		}
		var lateID ID
		_, err := h.CommitTasks(bg, owned, func(tx *Tx) error {
			var err error
			lateID, err = tx.CreateTask(late, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, lateEntered)
		close(earlyRelease)
		var earlyID ID
		state, err := h.Snapshot(caller)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			if task.Kind == early.Kind() {
				earlyID = task.ID
			}
		}
		waitPublicTask(t, h, earlyID)
		state, err = h.Snapshot(caller)
		if err != nil || state.Tasks[parentID].Status != "completing" || state.Tasks[lateID].Status != "running" || state.Conversations[owned].Owner != parentID || state.Tasks[parentID].Execution.Native.State.Outcome.Result.Value != "parent" {
			t.Fatal("late work escaped hold", state.Tasks[parentID], state.Tasks[lateID], err)
		}
		close(lateRelease)
		record := waitPublicTask(t, h, parentID)
		if record.State.Outcome.Status != "completed" || record.State.Outcome.Result.Value != "parent" {
			t.Fatal(record)
		}
		waitPublicTask(t, h, lateID)
	})
}

func TestTaskOwnershipPinnedFinishingBatchOwnedConversationWorkHoldsParent(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, release := make(chan struct{}), make(chan struct{})
		var childID ID
		child := taskDefinition(t, "task.finishing-conversation.child", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(entered)
			<-release
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("child"), nil })
		})
		parent := taskDefinition(t, "task.finishing-conversation.parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			owned, err := r.CreateOwnedConversation(ctx, "owned", nil)
			if err != nil {
				return err
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				var err error
				childID, err = tx.CreateTask(child, nil, TaskOptions{Conversation: owned.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
				if err != nil {
					return nil, err
				}
				return taskDone("parent"), nil
			})
		})
		h := taskTestHarness(t, b.store, parent, child)
		cleanupTaskGates(t, release)
		if _, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}}); err != nil {
			t.Fatal(err)
		}
		parentID := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[parentID].Status != "completing" || state.Tasks[childID].Owner != 0 || state.Conversations[state.Tasks[childID].Conversation].Owner != parentID {
			t.Fatal("finishing conversation hold", state.Tasks[parentID], state.Tasks[childID], err)
		}
		close(release)
		record := waitPublicTask(t, h, parentID)
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
	})
}
