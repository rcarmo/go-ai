package durable

import (
	"context"
	"testing"
)

func TestConversationInitializersBindTaskCreationToRootNewAndFork(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		definition := taskDefinition(t, "task.init-bound", func(context.Context, TaskRecord, *TaskRuntime) error { t.Error("init dispatched task"); return nil })
		h := taskTestHarness(t, b.store, definition)
		created := map[ID]ID{}
		init := func(tx *Tx, id ID) error {
			task, err := tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			created[id] = task
			return err
		}
		root, err := h.RootWithInit(bg, AgentChange{}, init)
		if err != nil {
			t.Fatal(err)
		}
		child, err := h.CreateConversationWithInit(bg, AgentChange{}, init)
		if err != nil {
			t.Fatal(err)
		}
		var at ID
		_, err = root.Commit(bg, func(tx *Tx) error {
			var err error
			at, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: at, Conversation: root.ID(), Kind: "note", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		fork, err := root.ForkWithInit(bg, at, nil, init)
		if err != nil {
			t.Fatal(err)
		}
		for _, handle := range []*ConversationHandle{root, child, fork} {
			task, ok, err := h.Task(bg, created[handle.ID()])
			if err != nil || !ok || task.Conversation != handle.ID() || task.State.Status != "pending" {
				t.Fatal("init task binding", task, err)
			}
		}
		if h.scheduler.enabled.Load() {
			t.Fatal("initialization enabled scheduler")
		}
	})
}
