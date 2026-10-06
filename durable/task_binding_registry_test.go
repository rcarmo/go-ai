package durable

import (
	"context"
	"sync/atomic"
	"testing"
)

func TestTaskLifecyclePinnedRegistryReplacementBeforeResumeAndConversationBinding(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var oldRuns, newRuns atomic.Int64
		original := taskDefinition(t, "task.before-resume", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			oldRuns.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("old"), nil })
		})
		h := taskTestHarness(t, b.store)
		conversation, err := h.CreateConversation(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		var id ID
		_, err = conversation.Commit(bg, func(tx *Tx) error {
			var err error
			id, err = tx.CreateTask(original, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		task, ok, err := h.Task(bg, id)
		if err != nil || !ok || task.Conversation != conversation.ID() {
			t.Fatal("conversation binding lost", task, err)
		}
		_, err = h.CommitTasks(bg, 0, func(tx *Tx) error {
			_, err := tx.CreateTask(original, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err == nil {
			t.Fatal("unbound task creation defaulted to root")
		}
		var explicit ID
		_, err = h.CommitTasks(bg, 0, func(tx *Tx) error {
			var err error
			explicit, err = tx.CreateTask(original, nil, TaskOptions{Conversation: conversation.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		// A passive commit may land while another task is already pending.
		_, err = conversation.Commit(bg, func(tx *Tx) error {
			entry, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: entry, Conversation: conversation.ID(), Kind: "note", Value: JSON{}})
		})
		if err != nil {
			t.Fatal("live conversation commit rejected", err)
		}
		dispose, err := h.options.Registry.RegisterTask(original)
		if err != nil {
			t.Fatal(err)
		}
		options := original.options
		options.Phases = map[string]TaskPhase{"work": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			newRuns.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("new"), nil })
		}}
		replacement, err := DefineTask(options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.options.Registry.RegisterTask(replacement); err != nil {
			t.Fatal(err)
		}
		dispose() // Disposing the old identity cannot remove the replacement.
		view, err := h.Inspect(bg)
		if err != nil || view.Scheduling != "paused" || oldRuns.Load() != 0 || newRuns.Load() != 0 {
			t.Fatal("registry/read started effects", view, err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{id, explicit} {
			final := waitPublicTask(t, h, id)
			if final.State.Outcome.Result.Value != "new" || final.Conversation != conversation.ID() {
				t.Fatal(final)
			}
		}
		if oldRuns.Load() != 0 || newRuns.Load() != 2 {
			t.Fatal("wrong pre-resume registry", oldRuns.Load(), newRuns.Load())
		}
	})
}
