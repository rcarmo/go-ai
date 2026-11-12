package durable

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

// Literal read-before-first-table-write contract from session-tables.test.ts:85
// at f9a26365. No staged-table read contract is carried forward.
func TestTxV150TaskAndTableReadsBeforeFirstTableWrite(t *testing.T) {
	t.Run("literal-source-packet", func(t *testing.T) {
		store := taskConformanceStoreV150(t)
		session, err := OpenSession(store)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close(bg)
		notes := mustDefinition(t, DefinitionOptions{Kind: "source.table.notes", Scope: "conversation", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
		_, err = session.Commit(bg, func(tx *Tx) error {
			conversation, found, err := tx.Conversation(1)
			if err != nil || !found || !reflect.DeepEqual(conversation, Conversation{ID: 1}) {
				t.Fatal(conversation, found, err)
			}
			conversations, err := tx.ScanConversations(ConversationQuery{}, 1, nil)
			if err != nil || !reflect.DeepEqual(conversations.Items, []Conversation{{ID: 1}}) || conversations.Next != nil {
				t.Fatal(conversations, err)
			}
			tasks, err := tx.ScanTasks(TaskQuery{Conversation: 1}, 10, nil)
			if err != nil || len(tasks.Items) != 0 || tasks.Next != nil {
				t.Fatal(tasks, err)
			}
			entries, err := tx.ScanEntries(EntryQuery{Conversation: 1}, 10, nil)
			if err != nil || len(entries.Items) != 0 || entries.Next != nil {
				t.Fatal(entries, err)
			}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note"}); err != nil {
				return err
			}
			for _, read := range []func() error{
				func() error { _, _, err := tx.Conversation(1); return err },
				func() error { _, _, err := tx.Entry(1); return err },
				func() error { _, err := tx.ScanConversations(ConversationQuery{}, 10, nil); return err },
				func() error { _, err := tx.ScanEntries(EntryQuery{Conversation: 1}, 10, nil); return err },
			} {
				var fenced *ReadAfterWrite
				if err := read(); !errors.As(err, &fenced) {
					t.Fatal("source ReadAfterWrite type", err)
				}
			}
			if _, _, err := tx.Task(1); err == nil || err.Error() != "Tx.task() cannot read tables after the first table write" {
				t.Fatal(err)
			}
			doc, err := tx.AcquireDocument(notes, 1, nil, nil)
			if err != nil {
				return err
			}
			return doc.Set(JSON{"text": "after write"})
		})
		if err != nil {
			t.Fatal(err)
		}
		value, found, err := session.SnapshotDefinition(bg, notes, 1, nil)
		if err != nil || !found || !equalJSONValue(value.Copy(), JSON{"text": "after write"}) {
			t.Fatal(value, found, err)
		}
	})
	// Additional committed Task and scan/fence controls retained below.
	store := taskConformanceStoreV150(t)
	task := conformanceTaskV150(mint(t, store))
	storageTasksCommitV150(t, store, task)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) error {
		conversation, found, err := tx.Conversation(1)
		if err != nil || !found || conversation.ID != 1 {
			t.Fatal(conversation, found, err)
		}
		if _, found, err := tx.Entry(1); err != nil || found {
			t.Fatal(found, err)
		}
		before, found, err := tx.Task(task.ID)
		if err != nil || !found {
			t.Fatalf("task before write found=%v err=%v", found, err)
		}
		requireStorageTaskEqualV150(t, before, task)
		if _, err := tx.ScanTasks(TaskQuery{Conversation: 1}, 10, nil); err != nil {
			return err
		}
		if _, err := tx.ScanConversations(ConversationQuery{}, 10, nil); err != nil {
			return err
		}
		if _, err := tx.ScanEntries(EntryQuery{Conversation: 1}, 10, nil); err != nil {
			return err
		}
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
			return err
		}
		checks := []struct {
			name string
			read func() error
		}{
			{"conversation", func() error { _, _, err := tx.Conversation(1); return err }},
			{"entry", func() error { _, _, err := tx.Entry(id); return err }},
			{"task", func() error { _, _, err := tx.Task(task.ID); return err }},
			{"scanTasks", func() error { _, err := tx.ScanTasks(TaskQuery{}, 10, nil); return err }},
			{"scanConversations", func() error { _, err := tx.ScanConversations(ConversationQuery{}, 10, nil); return err }},
			{"scanEntries", func() error { _, err := tx.ScanEntries(EntryQuery{Conversation: 1}, 10, nil); return err }},
			{"scanSubmissions", func() error { _, err := tx.ScanSubmissions(SubmissionQuery{Conversation: 1}, 10, nil); return err }},
			{"latestEntryId", func() error { _, _, err := tx.LatestEntryID(1); return err }},
			{"latestHeadMarker", func() error { _, _, err := tx.LatestHeadMarker(1, 0); return err }},
		}
		for _, check := range checks {
			err := check.read()
			want := "Tx." + check.name + "() cannot read tables after the first table write"
			if err == nil || err.Error() != want {
				t.Errorf("%s got=%v want=%s", check.name, err, want)
			}
		}
		// Document writes remain legal after table writes.
		docID, err := tx.MintID()
		if err != nil {
			return err
		}
		doc, err := tx.CreateDocument(Document{ID: docID, Kind: "v150.reads", Scope: "session", Version: 1, Value: JSON{"text": "before"}})
		if err != nil {
			return err
		}
		return doc.Set(JSON{"text": "after write"})
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Source-derived guard ordering control, paired with the retained independent
// reference probe. A caught failed table write still sets #hasTableWrite.
func TestTxV150DocumentWritesAllowReadsButFailedTableWriteFencesThem(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		doc, err := tx.CreateDocument(Document{ID: id, Kind: "v150.table.read.fence", Scope: "session", Version: 1, Value: JSON{"text": "before"}})
		if err != nil {
			return err
		}
		if err = doc.Set(JSON{"text": "document write"}); err != nil {
			return err
		}
		if _, found, err := tx.Task(99); err != nil || found {
			t.Fatalf("document write fenced table read: found=%v err=%v", found, err)
		}
		if page, err := tx.ScanTasks(TaskQuery{}, 10, nil); err != nil || len(page.Items) != 0 {
			t.Fatalf("document write fenced task scan: %v %v", page, err)
		}
		bad, err := tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: bad, Conversation: 12345, Kind: "note", Value: JSON{}}); err == nil {
			t.Fatal("missing conversation accepted")
		}
		for _, taskID := range []ID{99, ID(MaxID + 1)} {
			_, _, err = tx.Task(taskID)
			if err == nil || err.Error() != "Tx.task() cannot read tables after the first table write" {
				t.Fatalf("failed write did not fence task read: %v", err)
			}
		}
		value, err := doc.Get()
		if err != nil {
			return err
		}
		if value["text"] != "document write" {
			t.Fatal(value)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTaskTablesPinnedStagedOwnerSameBatchDocumentsAndLegacyReplacement(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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

func TestTaskTablesPinnedTypedSameBatchOwnerAndLaterPublication(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		work := taskDefinition(t, "task.table.typed", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		progress, err := DefineDocument(DefinitionOptions{CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }, Kind: "app.table.progress", Version: 1, Scope: "task", Initial: func(JSON) (JSON, error) { return JSON{"lines": []any{}}, nil }})
		if err != nil {
			t.Fatal(err)
		}
		step, err := DefineDocument(DefinitionOptions{CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }, Kind: "app.table.step", Version: 1, Scope: "task", Family: true, Initial: func(JSON) (JSON, error) { return JSON{"lines": []any{}}, nil }})
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

// Pinned transaction.ts:297-305 narrows by kind only, using a global committed
// entry lookup. Conversation visibility belongs to Storage.entry's other overload.
func TestTxTypedEntryReadsGlobalCommittedRecordAndNarrowsKind(t *testing.T) {
	s := taskConformanceStoreV150(t)
	other, id := mint(t, s), mint(t, s)
	result, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: other}}, {Type: "entry", Value: Entry{ID: id, Conversation: other, Kind: "foreign.note", HasData: true, Data: JSON{"value": "owned"}}}}}, nil)
	if err != nil || result.Refused != nil {
		t.Fatal(result, err)
	}
	session, err := OpenSession(s)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	token := entryToken(t, "foreign.note")
	wrong := entryToken(t, "other.kind")
	_, err = session.Commit(bg, func(tx *Tx) error {
		record, found, err := tx.TypedEntry(token, id)
		if err != nil || !found || record.Conversation != other {
			t.Fatal(record, found, err)
		}
		record.Data.(map[string]any)["value"] = "mutated"
		if _, found, err := tx.TypedEntry(wrong, id); err != nil || found {
			t.Fatal(found, err)
		}
		record, found, err = tx.TypedEntry(token, id)
		if err != nil || !found || record.Data.(map[string]any)["value"] != "owned" {
			t.Fatal(record, found, err)
		}
		fresh, err := tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: fresh, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
			return err
		}
		_, _, err = tx.TypedEntry(token, id)
		if err == nil || err.Error() != "Tx.entry() cannot read tables after the first table write" {
			t.Fatal("typed read bypassed table-write fence", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Source transaction.ts:333-343 and #read: submission reads are committed tables.
func TestTxSubmissionLookupFencesAfterTableWrite(t *testing.T) {
	s := taskConformanceStoreV150(t)
	id := mint(t, s)
	submissionCommit(t, s, SubmissionRecord{ID: id, Conversation: 1, Type: "input", Status: "queued"})
	session, err := OpenSession(s)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) error {
		actual, found, err := tx.Submission(id)
		if err != nil || !found || actual.Status != "queued" {
			t.Fatal(actual, found, err)
		}
		if err := tx.PutSubmission(Submission{ID: id, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{}}); err != nil {
			return err
		}
		_, _, err = tx.Submission(id)
		var fenced *ReadAfterWrite
		if !errors.As(err, &fenced) || fenced.Method != "submission" {
			t.Fatalf("submission read bypassed source table fence: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestTxSubmissionByRequestCommittedLookupAndFence(t *testing.T) {
	s := taskConformanceStoreV150(t)
	id := mint(t, s)
	submissionCommit(t, s, SubmissionRecord{ID: id, Conversation: 1, Type: "continue", Status: "placed", RequestIDPresent: true})
	session, err := OpenSession(s)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) error {
		reader := tx
		actual, found, err := reader.SubmissionByRequest(1, "")
		if err != nil || !found || actual.ID != id || !actual.RequestIDPresent {
			t.Fatal(actual, found, err)
		}
		if _, found, err := reader.SubmissionByRequest(99, ""); err != nil || found {
			t.Fatal(found, err)
		}
		fresh, err := tx.MintID()
		if err != nil {
			return err
		}
		if err = tx.AppendEntry(Entry{ID: fresh, Conversation: 1, Kind: "note"}); err != nil {
			return err
		}
		_, _, err = reader.SubmissionByRequest(1, "")
		var fenced *ReadAfterWrite
		if !errors.As(err, &fenced) || fenced.Method != "submissionByRequest" {
			t.Fatal(err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
