package durable

import (
	"context"
	"testing"
)

func TestTaskOwnershipPinnedCreateRejectMatrixAndFinalOwnerOverlay(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		definition := taskDefinition(t, "task.creation.boundary", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		h := taskTestHarness(t, b.store, definition)
		parent := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		var other ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			other, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: other})
		})
		if err != nil {
			t.Fatal(err)
		}
		for name, options := range map[string]TaskOptions{"no-owner": {}, "missing": {Ownership: TaskOwnership{Kind: "task", Task: ID(MaxID)}}, "different-conversation": {Conversation: other, Ownership: TaskOwnership{Kind: "task", Task: parent}}, "background-child": {Background: true, Ownership: TaskOwnership{Kind: "task", Task: parent}}} {
			t.Run(name, func(t *testing.T) {
				before, _ := h.Snapshot(bg)
				_, err := h.CommitTasks(bg, 1, func(tx *Tx) error { _, err := tx.CreateTask(definition, nil, options); return err })
				if err == nil {
					t.Fatal("invalid creation accepted")
				}
				after, _ := h.Snapshot(bg)
				if after.Seq != before.Seq || after.HighWater != before.HighWater || len(after.Tasks) != len(before.Tasks) {
					t.Fatal("invalid creation wrote", before.Seq, after.Seq)
				}
			})
		}
		_, err = h.CommitTasks(bg, other, func(tx *Tx) error {
			id, err := tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: parent}})
			if err != nil {
				return err
			}
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			if task := candidate.Tasks[id]; task.Conversation != 1 || task.Owner != parent || taskBackground(task) {
				t.Error("child binding", task)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		// Separate real runtime fixture: child is staged before a terminal state in
		// the same commit; final-owner validation rejects the entire candidate.
		ending := taskDefinition(t, "task.creation.ending", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				if _, err := tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}}); err != nil {
					return nil, err
				}
				return taskDone("finish"), nil
			})
		})
		if _, err := h.options.Registry.RegisterTask(ending); err != nil {
			t.Fatal(err)
		}
		endingID := createPublicTask(t, h, ending, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, endingID)
		if record.State.Outcome.Status != "faulted" {
			t.Fatal("finishing child admission", record)
		}
		snapshot, _ := h.Snapshot(bg)
		for _, task := range snapshot.Tasks {
			if task.Owner == endingID {
				t.Fatal("rejected child leaked", task)
			}
		}
	})
}
