package durable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Consolidated from transaction_tables_test.go.

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

// Consolidated from submission_transaction_test.go.

func TestSubmissionTransactionCurrentRecordPlacementFirstSettlementAndDetached(t *testing.T) {
	// Literal harness-submissions.test.ts:228-266 at f9a26365.
	t.Run("literal-source-packet", func(t *testing.T) {
		store := taskConformanceStoreV150(t)
		h := taskTestHarness(t, store)
		root, err := h.Conversation(bg, 1)
		if err != nil {
			t.Fatal(err)
		}
		var entry ID
		_, err = root.Commit(bg, func(tx *Tx) error {
			var err error
			entry, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "note"})
		})
		if err != nil {
			t.Fatal(err)
		}
		create := func(kind string) ID {
			t.Helper()
			var record SubmissionRecord
			_, err := h.Commit(bg, func(tx *Tx) error {
				var err error
				record, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: kind, Status: "queued"})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			return record.ID
		}
		queued, write := create("input"), create("write")
		for _, id := range []ID{queued, write} {
			_, err = root.Commit(bg, func(tx *Tx) error {
				return tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: entry})
			})
			if err == nil || !strings.Contains(err.Error(), "is not a placed input") {
				t.Fatal(err)
			}
		}
		_, err = root.Commit(bg, func(tx *Tx) error {
			return tx.SettleSubmission(999999, SubmissionSettlement{Status: "unanswered", Reason: "x"})
		})
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatal(err)
		}
		var placed SubmissionRecord
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			placed, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "placed", Entry: entry})
			if err != nil {
				return err
			}
			if err = tx.SettleSubmission(placed.ID, SubmissionSettlement{Status: "done", Answer: entry}); err != nil {
				return err
			}
			return tx.SettleSubmission(placed.ID, SubmissionSettlement{Status: "unanswered", Reason: "late"})
		})
		if err != nil {
			t.Fatal(err)
		}
		handle, err := h.Submission(bg, placed.ID)
		if err != nil {
			t.Fatal(err)
		}
		actual, err := handle.Status(bg)
		if err != nil || actual.Status != "done" || actual.Entry != entry || actual.Answer != entry {
			t.Fatal(actual, err)
		}
	})

	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		var entry, input, write, placed ID
		_, err := h.Commit(bg, func(tx *Tx) error {
			var err error
			entry, err = tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
				return err
			}
			createdInput, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued"})
			input = createdInput.ID
			if err != nil {
				return err
			}
			createdWrite, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "write", Status: "queued"})
			write = createdWrite.ID
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{input, write} {
			if _, err := h.Commit(bg, func(tx *Tx) error {
				return tx.SettleSubmission(id, SubmissionSettlement{Status: "unanswered", Reason: "aborted", Answer: entry})
			}); err == nil {
				t.Fatal("unanswered settlement accepted answer", id)
			}
			_, err := h.Commit(bg, func(tx *Tx) error {
				return tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: entry})
			})
			if err == nil {
				t.Fatal("unplaced submission answered", id)
			}
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			return tx.SettleSubmission(ID(MaxID), SubmissionSettlement{Status: "unanswered", Reason: "missing"})
		})
		if err == nil {
			t.Fatal("missing submission settled")
		}
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			createdPlaced, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "placed", Entry: entry})
			placed = createdPlaced.ID
			if err != nil {
				return err
			}
			if err := tx.SettleSubmission(placed, SubmissionSettlement{Status: "done", Answer: entry}); err != nil {
				return err
			}
			if err := tx.SettleSubmission(placed, SubmissionSettlement{Status: "unanswered", Reason: "late"}); err != nil {
				return err
			}
			if _, _, err := tx.Submission(placed); err == nil {
				t.Fatal("staged submission read")
			}
			if err := tx.PlaceSubmission(input, entry); err != nil {
				return err
			}
			return tx.PlaceSubmission(write, entry)
		})
		if err != nil {
			t.Fatal(err)
		}
		record, found, err := h.session.Submission(bg, placed)
		if err != nil || !found || record.Status != "done" || record.Entry != entry || record.Answer != entry {
			t.Fatal(record, found, err)
		}
		record.Entry = ID(MaxID)
		againPlaced, found, err := h.session.Submission(bg, placed)
		if err != nil || !found || againPlaced.Entry != entry {
			t.Fatal("record alias", againPlaced, err)
		}
		var unanswered ID
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			createdUnanswered, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued"})
			unanswered = createdUnanswered.ID
			if err != nil {
				return err
			}
			if err := tx.SettleSubmission(unanswered, SubmissionSettlement{Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true, Value: nil}}); err != nil {
				return err
			}
			if _, _, err := tx.Submission(unanswered); err == nil {
				t.Fatal("staged submission read")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		nullHandle, err := h.Submission(bg, unanswered)
		if err != nil {
			t.Fatal(err)
		}
		nullRecord, err := nullHandle.Status(bg)
		if err != nil || nullRecord.Detail == nil || !nullRecord.Detail.Present || nullRecord.Detail.Value != nil {
			t.Fatal("committed null detail lost", nullRecord, err)
		}
		nullRecord.Detail.Value = JSON{"mutated": true}
		again, err := nullHandle.Status(bg)
		if err != nil || again.Detail == nil || again.Detail.Value != nil {
			t.Fatal("detail retrieval aliases storage", again, err)
		}
		candidate := JSON{"nested": []any{"kept"}}
		var detailed ID
		_, err = h.Commit(bg, func(tx *Tx) error {
			var err error
			createdDetailed, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true, Value: candidate}})
			detailed = createdDetailed.ID
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		candidate["nested"].([]any)[0] = "caller mutation"
		detailHandle, err := h.Submission(bg, detailed)
		if err != nil {
			t.Fatal(err)
		}
		detailRecord, err := detailHandle.Status(bg)
		if err != nil || detailRecord.Detail == nil || !equalJSONValue(detailRecord.Detail.Value, JSON{"nested": []any{"kept"}}) {
			t.Fatal("detail candidate aliased", detailRecord, err)
		}
		detailRecord.Detail.Value.(map[string]any)["nested"].([]any)[0] = "read mutation"
		detailAgain, err := detailHandle.Status(bg)
		if err != nil || !equalJSONValue(detailAgain.Detail.Value, JSON{"nested": []any{"kept"}}) {
			t.Fatal("detail read aliased", detailAgain, err)
		}
		for _, id := range []ID{placed, input, write} {
			sub, err := h.Submission(bg, id)
			if err != nil {
				t.Fatal(err)
			}
			record, err := sub.Status(bg)
			if err != nil || record.Entry != entry {
				t.Fatal(record, err)
			}
			if id == input && record.Status != "placed" || id != input && record.Status != "done" {
				t.Fatal(record)
			}
		}
	})
}

// Source transaction.ts:526-540 defers record lookup and transition errors until
// assembly. A caught/ignored callback method error cannot turn a failed change
// into a successful empty commit.
func TestTxSubmissionChangesResolveOnlyDuringAssembly(t *testing.T) {
	for _, placement := range []bool{false, true} {
		t.Run(fmt.Sprint(placement), func(t *testing.T) {
			store := taskConformanceStoreV150(t)
			session, err := OpenSession(store)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(bg)
			finished := false
			_, err = session.Commit(bg, func(tx *Tx) error {
				var err error
				if placement {
					err = tx.PlaceSubmission(999999, 2)
				} else {
					err = tx.SettleSubmission(999999, SubmissionSettlement{Status: "unanswered", Reason: "x"})
				}
				if err != nil {
					t.Fatal("submission change resolved inside callback", err)
				}
				finished = true
				return nil
			})
			if !finished || err == nil || !strings.Contains(err.Error(), "does not exist") {
				t.Fatal("missing assembly failure", finished, err)
			}
		})
	}
}

// Source createSubmission requires the conversation before minting, and #write
// fences table reads even when the caller catches that creation failure.
func TestTxSubmissionFailedCreateRequiresConversationBeforeMint(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) error {
		high := tx.state.HighWater
		if _, err := tx.CreateSubmission(SubmissionCreate{Conversation: 999999, Type: "write", Status: "queued"}); err == nil {
			t.Fatal("missing conversation accepted during creation")
		}
		if tx.state.HighWater != high {
			t.Fatal("failed conversation lookup minted an ID")
		}
		_, _, err := tx.Submission(1)
		var fenced *ReadAfterWrite
		if !errors.As(err, &fenced) {
			t.Fatal("failed creation did not fence table reads", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Direct typed source surface: creation returns the complete record and exposes
// neither caller-assigned identity nor native compatibility payload fields.
func TestTxSubmissionCreateReturnsRecordSurface(t *testing.T) {
	for _, check := range []struct {
		typ   reflect.Type
		field string
	}{{reflect.TypeFor[SubmissionCreate](), "ID"}, {reflect.TypeFor[SubmissionCreate](), "Value"}, {reflect.TypeFor[SubmissionRecord](), "Value"}, {reflect.TypeFor[SubmissionSettlement](), "Entry"}} {
		if _, found := check.typ.FieldByName(check.field); found {
			t.Fatal("native field on source API", check.typ, check.field)
		}
	}
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	detail := JSON{"nested": []any{"owned"}}
	trace := goai.TelemetryCarrier{"traceparent": "owned"}
	var created SubmissionRecord
	_, err = session.Commit(bg, func(tx *Tx) error {
		var err error
		created, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "unanswered", Reason: "", RequestIDPresent: true, Detail: &TaskValue{Present: true, Value: detail}, Trace: &trace})
		if err != nil {
			return err
		}
		detail["nested"].([]any)[0] = "caller mutation"
		trace["traceparent"] = "caller mutation"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := SubmissionRecord{ID: created.ID, Conversation: 1, Type: "input", Status: "unanswered", Reason: "", RequestIDPresent: true, Detail: &TaskValue{Present: true, Value: JSON{"nested": []any{"owned"}}}, Trace: &goai.TelemetryCarrier{"traceparent": "owned"}}
	if created.ID == 0 {
		t.Fatal("missing created identity")
	}
	requireSubmissionEqual(t, created, want)
	created.Detail.Value.(map[string]any)["nested"].([]any)[0] = "returned mutation"
	(*created.Trace)["traceparent"] = "returned mutation"
	stored, found, err := session.Submission(bg, created.ID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	requireSubmissionEqual(t, stored, want)
}
func TestTxSubmissionTerminalChangesAreNoops(t *testing.T) {
	for _, kind := range []string{"input", "write", "continue"} {
		t.Run(kind, func(t *testing.T) {
			store := taskConformanceStoreV150(t)
			id := mint(t, store)
			want := SubmissionRecord{ID: id, Conversation: 1, Type: kind, Status: "unanswered", Reason: "first"}
			submissionCommit(t, store, want)
			session, err := OpenSession(store)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close(bg)
			before, err := session.Snapshot(bg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = session.Commit(bg, func(tx *Tx) error {
				if err := tx.PlaceSubmission(id, ID(MaxID)); err != nil {
					return err
				}
				return tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: ID(MaxID)})
			})
			if err != nil {
				t.Fatal("terminal record was revalidated as live", err)
			}
			actual, found, err := session.Submission(bg, id)
			if err != nil || !found {
				t.Fatal(found, err)
			}
			requireSubmissionEqual(t, actual, want)
			after, err := session.Snapshot(bg)
			if err != nil || after.Seq != before.Seq {
				t.Fatal("noop terminal changes wrote a record", after.Seq, before.Seq, err)
			}
		})
	}
}

func TestTxSubmissionAnswerReferenceDoesNotAddNativeVisibilityPolicy(t *testing.T) {
	store := taskConformanceStoreV150(t)
	id := mint(t, store)
	submissionCommit(t, store, SubmissionRecord{ID: id, Conversation: 1, Type: "continue", Status: "placed"})
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) error {
		return tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: ID(MaxID)})
	})
	if err != nil {
		t.Fatal("source settlement does not require a persisted/visible answer", err)
	}
	actual, found, err := session.Submission(bg, id)
	if err != nil || !found || actual.Answer != ID(MaxID) || actual.Status != "done" {
		t.Fatal(actual, found, err)
	}
}

func TestTxSubmissionQueuedChangesCanPrecedeTheirCreation(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	var created SubmissionRecord
	_, err = session.Commit(bg, func(tx *Tx) error {
		// Source changes resolve against the map of all creations at assembly, not
		// against the records present when this method is called.
		future := ID(tx.state.HighWater + 1)
		if err := tx.PlaceSubmission(future, ID(MaxID-1)); err != nil {
			return err
		}
		if err := tx.SettleSubmission(future, SubmissionSettlement{Status: "done", Answer: ID(MaxID)}); err != nil {
			return err
		}
		created, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued"})
		if err != nil {
			return err
		}
		if created.ID != future {
			t.Fatal(created.ID, future)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := session.Submission(bg, created.ID)
	if err != nil || !found || got.Status != "done" || got.Entry != ID(MaxID-1) || got.Answer != ID(MaxID) {
		t.Fatal(got, found, err)
	}
}
func TestTxSubmissionAssemblyFailurePrecedesDocumentPredicates(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	predicates := 0
	notes := mustDefinition(t, DefinitionOptions{Kind: "source.submission.validation.order", Scope: "conversation", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"text": ""}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { predicates++; return false, nil }})
	_, err = session.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(notes, 1, nil, nil); return err })
	if err != nil {
		t.Fatal(err)
	}
	before, err := session.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	predicates = 0
	finished := false
	_, err = session.Commit(bg, func(tx *Tx) error {
		doc, err := tx.AcquireDocument(notes, 1, nil, nil)
		if err != nil {
			return err
		}
		if err = doc.Set(JSON{"text": "must roll back"}); err != nil {
			return err
		}
		if err = tx.SettleSubmission(999999, SubmissionSettlement{Status: "unanswered", Reason: "x"}); err != nil {
			t.Fatal("early transition lookup", err)
		}
		finished = true
		return nil
	})
	if err == nil || !finished || predicates != 0 {
		t.Fatal("invalid submission reached predicates", finished, predicates, err)
	}
	after, err := session.Snapshot(bg)
	if err != nil || !equalSnapshot(before, after) {
		t.Fatal("assembly failure changed committed state", err)
	}
}
