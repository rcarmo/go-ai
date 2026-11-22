package durable

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
)

// harness-ownership.test.ts:582. The spawner stages a conversation and its
// first task, then tries to end itself in that same commit.
func TestSourceV160ConversationCreationRejectsEndingOwner(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var childRuns atomic.Int64
		child := taskDefinition(t, "v160.owner-child", func(context.Context, TaskRecord, *TaskRuntime) error {
			childRuns.Add(1)
			return errors.New("rejected child must not run")
		})
		spawner := taskDefinition(t, "v160.spawner", func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				conversation, err := tx.CreateConversation(JSON{"ownership": JSON{"kind": "task", "taskId": task.ID}})
				if err != nil {
					return nil, err
				}
				if _, err := tx.CreateTask(child, JSON{"name": "spawned"}, TaskOptions{Conversation: conversation.ID, Ownership: TaskOwnership{Kind: "conversation"}}); err != nil {
					return nil, err
				}
				return taskDone(nil), nil
			})
		})
		h := taskTestHarness(t, b.store, child, spawner)
		id := createPublicTask(t, h, spawner, JSON{"name": "spawned"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		got := waitPublicTask(t, h, id).State.Outcome
		want := fmt.Sprintf("Conversation owner task %d is ", id)
		if got == nil || got.Status != "faulted" || got.Result != nil || got.Reason != "" || got.Error == nil || got.Error.Detail != nil {
			t.Fatalf("spawner outcome: %+v, want faulted", got)
		}
		if message := got.Error.Message; message != want+"completing" && message != want+"terminal" {
			t.Fatalf("spawner fault: got %q, want %q followed by completing or terminal", message, want)
		}
		state, err := h.Snapshot(bg)
		if err != nil || len(state.Conversations) != 1 || len(state.Tasks) != 1 || childRuns.Load() != 0 {
			t.Fatalf("refused commit leaked: conversations=%d tasks=%d phases=%d err=%v", len(state.Conversations), len(state.Tasks), childRuns.Load(), err)
		}
	})
}
