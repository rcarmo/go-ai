package durable

import (
	"context"
	"testing"
)

func TestTaskTablesPinnedStagedOwnerSameBatchDocumentsAndLegacyReplacement(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		definition := taskDefinition(t, "task.table-new", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		var owner, conversation, document ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			owner, err = tx.CreateTask(definition, []any{"input"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
			if err != nil {
				return err
			}
			conversation, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: conversation, Owner: owner}); err != nil {
				return err
			}
			document, err = tx.MintID()
			if err != nil {
				return err
			}
			_, err = tx.CreateDocument(Document{ID: document, Scope: "task", Owner: owner, Kind: "app.same-commit", Version: 1, Value: JSON{"created": true}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Conversations[conversation].Owner != owner || state.Tasks[owner].Conversation != 1 || !taskBackground(state.Tasks[owner]) || state.Documents[document].Owner != owner || state.Documents[document].CreatedAt != state.Seq {
			t.Fatal("staged owner/document", state, err)
		}
		before := state.Seq
		_, err = h.CommitTasks(bg, 1, func(tx *Tx) error {
			task, err := copyTask(tx.state.Tasks[owner], tx.limits)
			if err != nil {
				return err
			}
			task.Conversation = conversation
			return tx.stage(Write{Op: "put-task", Task: &task})
		})
		if err == nil {
			t.Fatal("native conversation identity replaced")
		}
		state, _ = h.Snapshot(bg)
		if state.Seq != before {
			t.Fatal("identity rejection wrote")
		}
		// Low-level legacy rows remain complete replacement envelopes. Public native
		// records are invocation-gated and cannot be replaced with an arbitrary DTO.
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		t.Cleanup(func() { _ = store.Close(bg) })
		rawID := mint(t, store)
		raw := Task{ID: rawID, Conversation: 1, Kind: "app.raw-table", Status: "pending", Checkpoint: JSON{"drop": "old", "keep": 1}}
		apply(t, store, Write{Op: "put-task", Task: &raw})
		raw.Checkpoint = JSON{"keep": 2}
		apply(t, store, Write{Op: "put-task", Task: &raw})
		state = snap(t, store)
		if _, present := state.Tasks[rawID].Checkpoint["drop"]; present || !equalJSONValue(state.Tasks[rawID].Checkpoint["keep"], 2) {
			t.Fatal("legacy record merged", state.Tasks[rawID])
		}
	})
}
