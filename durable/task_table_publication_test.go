package durable

import (
	"context"
	"testing"
	"time"
)

func TestTaskTablesPinnedTypedSameBatchOwnerAndLaterPublication(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		work := taskDefinition(t, "task.table.typed", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		progress, err := DefineDocument(DefinitionOptions{Kind: "app.table.progress", Version: 1, Scope: "task", Initial: func(JSON) (JSON, error) { return JSON{"lines": []any{}}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		step, err := DefineDocument(DefinitionOptions{Kind: "app.table.step", Version: 1, Scope: "task", Family: true, Initial: func(JSON) (JSON, error) { return JSON{"lines": []any{}}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		_, subscription, err := h.session.SubscribeCommits(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer subscription.Stop()
		frames := make(chan PublicationFrame, 3)
		if err := subscription.Start(func(_ context.Context, frame PublicationFrame) error { frames <- frame; return nil }); err != nil {
			t.Fatal(err)
		}
		var owner, progressID, stepID ID
		key := "one"
		seq, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			owner, err = tx.CreateTask(work, JSON{"path": "a"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
			if err != nil {
				return err
			}
			// No caller table read between CreateTask and either typed acquisition.
			first, err := tx.AcquireDocument(progress, owner, nil, nil)
			if err != nil {
				return err
			}
			second, err := tx.AcquireDocument(step, owner, &key, nil)
			if err != nil {
				return err
			}
			progressID, stepID = first.id, second.id
			if err := first.Set(JSON{"lines": []any{"created"}}); err != nil {
				return err
			}
			return second.Set(JSON{"lines": []any{"step"}})
		})
		if err != nil {
			t.Fatal(err)
		}
		nextFrame := func() PublicationFrame {
			t.Helper()
			select {
			case frame := <-frames:
				return frame
			case <-time.After(3 * time.Second):
				t.Fatal("typed task-document publication missing")
				return PublicationFrame{}
			}
		}
		frame := nextFrame()
		if frame.Snapshot != nil || frame.Publication.Seq != seq || len(frame.Publication.Documents) != 2 {
			t.Fatal("same-batch publication", frame)
		}
		ids := map[ID]bool{}
		for _, change := range frame.Publication.Documents {
			if change.Record.Scope != "task" || change.Record.Owner != owner || change.Record.CreatedAt != seq || change.Record.Retired {
				t.Fatal("staged owner document projection", change)
			}
			ids[change.Record.ID] = true
		}
		if !ids[progressID] || !ids[stepID] {
			t.Fatal("typed publication IDs", ids)
		}
		writes := 0
		for _, write := range frame.Publication.Tables {
			if write.Task != nil && write.Task.ID == owner {
				writes++
				if write.Task.Conversation != 1 {
					t.Fatal("staged owner conversation", write.Task)
				}
			}
		}
		if writes != 1 {
			t.Fatal("same-batch task publication count", writes)
		}
		query := b.store.(DocumentHistoryStorage)
		for id, text := range map[ID]string{progressID: "created", stepID: "step"} {
			doc, found, err := query.DocumentAt(bg, id, CurrentDocumentPoint())
			if err != nil || !found || !equalJSONValue(doc.Value["lines"], []any{text}) {
				t.Fatal("committed typed task document", doc, found, err)
			}
		}
		// An unrelated ownerless conversation in this transaction must not become
		// the owner scope of a document belonging to the previously committed task.
		var other ID
		later, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			other, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: other}); err != nil {
				return err
			}
			doc, err := tx.AcquireDocument(progress, owner, nil, nil)
			if err != nil {
				return err
			}
			return doc.Set(JSON{"lines": []any{"created", "committed task"}})
		})
		if err != nil {
			t.Fatal(err)
		}
		frame = nextFrame()
		ownedChanges := 0
		for _, change := range frame.Publication.Documents {
			if change.Record.Scope == "task" {
				ownedChanges++
				if change.Record.Owner != owner || change.Record.ID != progressID {
					t.Fatal("wrong task document publication", change)
				}
			}
		}
		if frame.Publication.Seq != later || ownedChanges != 1 {
			t.Fatal("committed-owner publication", frame)
		}
		state, err := h.Snapshot(bg)
		if err != nil || state.Tasks[owner].Conversation != 1 || state.Conversations[other].Owner != 0 || !equalJSONValue(state.Documents[progressID].Value["lines"], []any{"created", "committed task"}) {
			t.Fatal("committed-owner scope/value", state, err)
		}
	})
}
