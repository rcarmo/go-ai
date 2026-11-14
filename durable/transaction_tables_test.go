package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
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
		_, err = session.Commit(bg, func(tx *Tx) (any, error) {
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
				return nil, err
			}
			if err = tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note"}); err != nil {
				return nil, err
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
				return nil, err
			}
			return nil, doc.Set(JSON{"text": "after write"})
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
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
			return nil, err
		}
		if _, err := tx.ScanConversations(ConversationQuery{}, 10, nil); err != nil {
			return nil, err
		}
		if _, err := tx.ScanEntries(EntryQuery{Conversation: 1}, 10, nil); err != nil {
			return nil, err
		}
		id, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		if err = tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
			return nil, err
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
			return nil, err
		}
		doc, err := tx.CreateDocument(Document{ID: docID, Kind: "v150.reads", Scope: "session", Version: 1, Value: JSON{"text": "before"}})
		if err != nil {
			return nil, err
		}
		return nil, doc.Set(JSON{"text": "after write"})
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		id, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		doc, err := tx.CreateDocument(Document{ID: id, Kind: "v150.table.read.fence", Scope: "session", Version: 1, Value: JSON{"text": "before"}})
		if err != nil {
			return nil, err
		}
		if err = doc.Set(JSON{"text": "document write"}); err != nil {
			return nil, err
		}
		if _, found, err := tx.Task(99); err != nil || found {
			t.Fatalf("document write fenced table read: found=%v err=%v", found, err)
		}
		if page, err := tx.ScanTasks(TaskQuery{}, 10, nil); err != nil || len(page.Items) != 0 {
			t.Fatalf("document write fenced task scan: %v %v", page, err)
		}
		bad, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		if err = tx.appendEntryRecord(Entry{ID: bad, Conversation: 12345, Kind: "note", Value: JSON{}}); err == nil {
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
			return nil, err
		}
		if value["text"] != "document write" {
			t.Fatal(value)
		}
		return nil, nil
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
		_, err := h.commitTasks(bg, 1, func(tx *Tx) error {
			var err error
			owner, err = tx.CreateTask(definition, []any{"input"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
			if err != nil {
				return err
			}
			conversation, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.createConversationRecord(Conversation{ID: conversation, Owner: owner}); err != nil {
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
		_, err = h.commitTasks(bg, 1, func(tx *Tx) error {
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
		seq, err := h.commitTasks(bg, 1, func(tx *Tx) error {
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
		later, err := h.commitTasks(bg, 1, func(tx *Tx) error {
			var err error
			other, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.createConversationRecord(Conversation{ID: other}); err != nil {
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
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
			return nil, err
		}
		if err = tx.appendEntryRecord(Entry{ID: fresh, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
			return nil, err
		}
		_, _, err = tx.TypedEntry(token, id)
		if err == nil || err.Error() != "Tx.entry() cannot read tables after the first table write" {
			t.Fatal("typed read bypassed table-write fence", err)
		}
		return nil, nil
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		actual, found, err := tx.Submission(id)
		if err != nil || !found || actual.Status != "queued" {
			t.Fatal(actual, found, err)
		}
		if err := tx.PutSubmission(Submission{ID: id, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{}}); err != nil {
			return nil, err
		}
		_, _, err = tx.Submission(id)
		var fenced *ReadAfterWrite
		if !errors.As(err, &fenced) || fenced.Method != "submission" {
			t.Fatalf("submission read bypassed source table fence: %v", err)
		}
		return nil, nil
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
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
			return nil, err
		}
		if err = tx.appendEntryRecord(Entry{ID: fresh, Conversation: 1, Kind: "note"}); err != nil {
			return nil, err
		}
		_, _, err = reader.SubmissionByRequest(1, "")
		var fenced *ReadAfterWrite
		if !errors.As(err, &fenced) || fenced.Method != "submissionByRequest" {
			t.Fatal(err)
		}
		return nil, nil
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
		_, err = root.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			entry, err = tx.MintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: entry, Conversation: 1, Kind: "note"})
		})
		if err != nil {
			t.Fatal(err)
		}
		create := func(kind string) ID {
			t.Helper()
			var record SubmissionRecord
			_, err := h.Commit(bg, func(tx *Tx) (any, error) {
				var err error
				record, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: kind, Status: "queued"})
				return nil, err
			})
			if err != nil {
				t.Fatal(err)
			}
			return record.ID
		}
		queued, write := create("input"), create("write")
		for _, id := range []ID{queued, write} {
			_, err = root.Commit(bg, func(tx *Tx) (any, error) {
				return nil, tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: entry})
			})
			if err == nil || !strings.Contains(err.Error(), "is not a placed input") {
				t.Fatal(err)
			}
		}
		_, err = root.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.SettleSubmission(999999, SubmissionSettlement{Status: "unanswered", Reason: "x"})
		})
		if err == nil || !strings.Contains(err.Error(), "does not exist") {
			t.Fatal(err)
		}
		var placed SubmissionRecord
		_, err = h.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			placed, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "placed", Entry: entry})
			if err != nil {
				return nil, err
			}
			if err = tx.SettleSubmission(placed.ID, SubmissionSettlement{Status: "done", Answer: entry}); err != nil {
				return nil, err
			}
			return nil, tx.SettleSubmission(placed.ID, SubmissionSettlement{Status: "unanswered", Reason: "late"})
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
		_, err := h.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			entry, err = tx.MintID()
			if err != nil {
				return nil, err
			}
			if err = tx.appendEntryRecord(Entry{ID: entry, Conversation: 1, Kind: "note", Value: JSON{}}); err != nil {
				return nil, err
			}
			createdInput, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued"})
			input = createdInput.ID
			if err != nil {
				return nil, err
			}
			createdWrite, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "write", Status: "queued"})
			write = createdWrite.ID
			return nil, err
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []ID{input, write} {
			if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
				return nil, tx.SettleSubmission(id, SubmissionSettlement{Status: "unanswered", Reason: "aborted", Answer: entry})
			}); err == nil {
				t.Fatal("unanswered settlement accepted answer", id)
			}
			_, err := h.Commit(bg, func(tx *Tx) (any, error) {
				return nil, tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: entry})
			})
			if err == nil {
				t.Fatal("unplaced submission answered", id)
			}
		}
		_, err = h.Commit(bg, func(tx *Tx) (any, error) {
			return nil, tx.SettleSubmission(ID(MaxID), SubmissionSettlement{Status: "unanswered", Reason: "missing"})
		})
		if err == nil {
			t.Fatal("missing submission settled")
		}
		_, err = h.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			createdPlaced, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "placed", Entry: entry})
			placed = createdPlaced.ID
			if err != nil {
				return nil, err
			}
			if err := tx.SettleSubmission(placed, SubmissionSettlement{Status: "done", Answer: entry}); err != nil {
				return nil, err
			}
			if err := tx.SettleSubmission(placed, SubmissionSettlement{Status: "unanswered", Reason: "late"}); err != nil {
				return nil, err
			}
			if _, _, err := tx.Submission(placed); err == nil {
				t.Fatal("staged submission read")
			}
			if err := tx.PlaceSubmission(input, entry); err != nil {
				return nil, err
			}
			return nil, tx.PlaceSubmission(write, entry)
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
		_, err = h.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			createdUnanswered, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued"})
			unanswered = createdUnanswered.ID
			if err != nil {
				return nil, err
			}
			if err := tx.SettleSubmission(unanswered, SubmissionSettlement{Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true, Value: nil}}); err != nil {
				return nil, err
			}
			if _, _, err := tx.Submission(unanswered); err == nil {
				t.Fatal("staged submission read")
			}
			return nil, nil
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
		_, err = h.Commit(bg, func(tx *Tx) (any, error) {
			var err error
			createdDetailed, err := tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "unanswered", Reason: "host", Detail: &TaskValue{Present: true, Value: candidate}})
			detailed = createdDetailed.ID
			return nil, err
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
			_, err = session.Commit(bg, func(tx *Tx) (any, error) {
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
				return nil, nil
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
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
		return nil, nil
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		created, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "unanswered", Reason: "", RequestIDPresent: true, Detail: &TaskValue{Present: true, Value: detail}, Trace: &trace})
		if err != nil {
			return nil, err
		}
		detail["nested"].([]any)[0] = "caller mutation"
		trace["traceparent"] = "caller mutation"
		return nil, nil
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
			_, err = session.Commit(bg, func(tx *Tx) (any, error) {
				if err := tx.PlaceSubmission(id, ID(MaxID)); err != nil {
					return nil, err
				}
				return nil, tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: ID(MaxID)})
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		return nil, tx.SettleSubmission(id, SubmissionSettlement{Status: "done", Answer: ID(MaxID)})
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		// Source changes resolve against the map of all creations at assembly, not
		// against the records present when this method is called.
		future := ID(tx.state.HighWater + 1)
		if err := tx.PlaceSubmission(future, ID(MaxID-1)); err != nil {
			return nil, err
		}
		if err := tx.SettleSubmission(future, SubmissionSettlement{Status: "done", Answer: ID(MaxID)}); err != nil {
			return nil, err
		}
		created, err = tx.CreateSubmission(SubmissionCreate{Conversation: 1, Type: "input", Status: "queued"})
		if err != nil {
			return nil, err
		}
		if created.ID != future {
			t.Fatal(created.ID, future)
		}
		return nil, nil
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
	_, err = session.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(notes, 1, nil, nil); return nil, err })
	if err != nil {
		t.Fatal(err)
	}
	before, err := session.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	predicates = 0
	finished := false
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(notes, 1, nil, nil)
		if err != nil {
			return nil, err
		}
		if err = doc.Set(JSON{"text": "must roll back"}); err != nil {
			return nil, err
		}
		if err = tx.SettleSubmission(999999, SubmissionSettlement{Status: "unanswered", Reason: "x"}); err != nil {
			t.Fatal("early transition lookup", err)
		}
		finished = true
		return nil, nil
	})
	if err == nil || !finished || predicates != 0 {
		t.Fatal("invalid submission reached predicates", finished, predicates, err)
	}
	after, err := session.Snapshot(bg)
	if err != nil || !equalSnapshot(before, after) {
		t.Fatal("assembly failure changed committed state", err)
	}
}

func TestTxSubmissionReadersUseInstalledWrapperAuthority(t *testing.T) {
	s := gatedSession(t)
	adapter := &submissionReadFaultStorage{Storage: s.store, session: s}
	s.taskBookkeeping(func() { s.store = adapter })
	_, err := s.Commit(bg, func(tx *Tx) (any, error) {
		record, found, err := tx.SubmissionByRequest(1, "")
		if err != nil || !found || record.ID != 44 {
			t.Fatal("transaction request read bypassed wrapper", record, found, err)
		}
		record, found, err = tx.Submission(2)
		if err != nil || !found || record.ID != 44 {
			t.Fatal("transaction record read bypassed wrapper", record, found, err)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Source harness-submit-in-commit.test.ts:232-248 callback rollback.
func TestSubmitInRollsBackWithCallerCommit(t *testing.T) {
	store := taskConformanceStoreV150(t)
	h := taskTestHarness(t, store)
	c, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	submit := c
	// openChat materialises its root builtin documents before the assertion baseline.
	if _, err = h.Commit(bg, func(tx *Tx) (any, error) { return nil, initializeBuiltins(tx, 1) }); err != nil {
		t.Fatal(err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("host check failed")
	var id ID
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		id, err = submit.SubmitIn(tx, Input{Type: "input", Content: "hi", RequestID: "r1"})
		if err != nil {
			return nil, err
		}
		return nil, failure
	})
	if !errors.Is(err, failure) || id == 0 {
		t.Fatal(id, err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq || len(after.Submissions) != len(before.Submissions) || len(after.Entries) != len(before.Entries) || len(after.Tasks) != len(before.Tasks) {
		t.Fatal("caller rollback leaked admission", before.Seq, after.Seq, err)
	}
	if live := mainContinueBoundaryDocument(t, after, 1, "pi.live"); len(generationObject(live["run"])) != 0 {
		t.Fatal("rollback started a run", live)
	}
	if _, found, err := h.session.SubmissionByRequest(bg, 1, "r1"); err != nil || found {
		t.Fatal(found, err)
	}
}

// Source harness-submit-in-commit.test.ts:403-419 ownership; a Go wrapper
// retaining the private owner is not the Session-created transaction object.
func TestSubmitInRejectsForeignAndCopiedTransactions(t *testing.T) {
	h := taskTestHarness(t, taskConformanceStoreV150(t))
	other := taskTestHarness(t, taskConformanceStoreV150(t))
	c, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	submit := c
	for _, foreign := range []bool{false, true} {
		owner := h
		if foreign {
			owner = other
		}
		_, err := owner.Commit(bg, func(tx *Tx) (any, error) {
			candidate := tx
			if !foreign {
				candidate = &Tx{session: tx.session, identity: tx.identity}
			}
			_, err := submit.SubmitIn(candidate, Input{Type: "write", Entry: &Entry{Kind: "app.note"}})
			return nil, err
		})
		if err == nil || !strings.Contains(err.Error(), "The transaction belongs to another Session") {
			t.Fatal("unowned transaction admitted", foreign, err)
		}
	}
}

// Source-derived from harness-submit-in-commit.test.ts:341-365: rejects every
// path after a table write, including dedup and a
// second admission. A host document write may precede admission.
func TestSubmitInReadAfterWriteFenceAndOneCommit(t *testing.T) {
	store := taskConformanceStoreV150(t)
	h := taskTestHarness(t, store)
	c, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	submit := c
	notes := mustDefinition(t, DefinitionOptions{Kind: "source.submit.sent", Scope: "conversation", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"id": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	var id ID
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		doc, err := tx.AcquireDocument(notes, 1, nil, nil)
		if err != nil {
			return nil, err
		}
		id, err = submit.SubmitIn(tx, Input{Type: "write", Entry: &Entry{Kind: "app.note", HasData: true, Data: JSON{"text": "committed"}}, RequestID: "r1"})
		if err != nil {
			return nil, err
		}
		if err = doc.Set(JSON{"id": int(id)}); err != nil {
			return nil, err
		}
		entry, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		return nil, tx.appendEntryRecord(Entry{ID: entry, Conversation: 1, Kind: "app.sent", HasData: true, Data: JSON{"submission": id}})
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq+1 || len(after.Submissions) != 1 || len(after.Entries) != 2 {
		t.Fatal("admission was not one commit", before.Seq, after.Seq, err)
	}
	value, found, err := h.SnapshotDefinition(bg, notes, 1, nil)
	if err != nil || !found || !equalJSONValue(value.Copy(), JSON{"id": int(id)}) {
		t.Fatalf("value=%#v id=%d found=%v err=%v", value.Copy(), id, found, err)
	}
	for _, second := range []bool{false, true} {
		_, err = h.Commit(bg, func(tx *Tx) (any, error) {
			if second {
				if _, err := submit.SubmitIn(tx, Input{Type: "write", Entry: &Entry{Kind: "app.note"}}); err != nil {
					return nil, err
				}
			} else {
				entry, err := tx.MintID()
				if err != nil {
					return nil, err
				}
				if err = tx.appendEntryRecord(Entry{ID: entry, Conversation: 1, Kind: "app.note"}); err != nil {
					return nil, err
				}
			}
			_, err := submit.SubmitIn(tx, Input{Type: "write", Entry: &Entry{Kind: "app.note"}, RequestID: "r1"})
			return nil, err
		})
		var fenced *ReadAfterWrite
		if !errors.As(err, &fenced) || err.Error() != "submit(tx, draft) cannot read tables after the first table write" {
			t.Fatal(second, err)
		}
	}
}

// Source harness-submit-in-commit.test.ts:322-340, unchanged admission/rejection
// assertions. A caught idle refusal must not consume a durable commit.
func TestSubmitInCaughtIdleRefusalHasNoWrites(t *testing.T) {
	h := taskTestHarness(t, taskConformanceStoreV150(t))
	c, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	// openChat materialises its root builtin documents before the assertion baseline.
	if _, err = h.Commit(bg, func(tx *Tx) (any, error) { return nil, initializeBuiltins(tx, 1) }); err != nil {
		t.Fatal(err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	outcome := ""
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		_, err := c.SubmitIn(tx, Input{Type: "input", Content: "hi", WhenBusy: "steer", WhenIdle: "reject"})
		if err == nil {
			outcome = "admitted"
		} else if errors.Is(err, ErrConversationIdle) {
			outcome = "idle"
		} else {
			outcome = "other"
		}
		return nil, nil
	})
	if err != nil || outcome != "idle" {
		t.Fatal(outcome, err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatal(before.Seq, after.Seq, err)
	}
	if live := mainContinueBoundaryDocument(t, after, 1, "pi.live"); len(generationObject(live["run"])) != 0 {
		t.Fatal("idle refusal started a run", live)
	}
}

func TestSubmitInCaughtTableFailureStillFencesAndEscapedTxRejects(t *testing.T) {
	h := taskTestHarness(t, taskConformanceStoreV150(t))
	c, err := h.Conversation(bg, 1)
	if err != nil {
		t.Fatal(err)
	}
	var escaped *Tx
	_, err = h.Commit(bg, func(tx *Tx) (any, error) { escaped = tx; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.SubmitIn(escaped, Input{Type: "input", Content: "late"}); !errors.Is(err, ErrSealed) {
		t.Fatal(err)
	}
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		if _, err := tx.CreateSubmission(SubmissionCreate{Conversation: 999, Type: "input"}); err == nil {
			t.Fatal("missing conversation accepted")
		}
		_, err := c.SubmitIn(tx, Input{Type: "input", Content: "late"})
		return nil, err
	})
	var fenced *ReadAfterWrite
	if !errors.As(err, &fenced) {
		t.Fatal(err)
	}
}

// Source harness-submit-in-commit.test.ts:309-320 and385-401: transaction
// dedup consumes no commit; a busy continue rejects, while whenIdle steers.
func TestSubmitInBusyContinueSteerAndDedup(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		select {
		case <-entered:
		default:
			close(entered)
		}
		go func() {
			select {
			case <-release:
				ch <- terminal("done")
			case <-ctx.Done():
			}
			close(ch)
		}()
		return ch
	})
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	c := root(t, h, ref)
	if _, err = c.Submit(bg, Input{Type: "input", Content: "work"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("model did not start")
	}
	_, err = h.Commit(bg, func(tx *Tx) (any, error) { _, err := c.SubmitIn(tx, Input{Type: "continue"}); return nil, err })
	if !errors.Is(err, ErrConversationBusy) {
		t.Fatal(err)
	}
	var steer ID
	draft := Input{Type: "input", Content: "steer", WhenBusy: "steer", WhenIdle: "reject", RequestID: "r1"}
	_, err = h.Commit(bg, func(tx *Tx) (any, error) { var err error; steer, err = c.SubmitIn(tx, draft); return nil, err })
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	inbox := mainContinueBoundaryDocument(t, before, c.ID(), "pi.inbox")
	items, ok := inbox["items"].([]any)
	if !ok || len(items) != 1 || submissionEntryID(generationObject(items[0])["id"]) != steer {
		t.Fatal("steer inbox IDs", inbox)
	}
	var again ID
	_, err = h.Commit(bg, func(tx *Tx) (any, error) { var err error; again, err = c.SubmitIn(tx, draft); return nil, err })
	if err != nil || again != steer {
		t.Fatal(steer, again, err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatal("dedup consumed a commit", before.Seq, after.Seq, err)
	}
}

// The source openBusy fixture: a real provider is held after the first input
// starts. Completion/cancellation signals, not elapsed time, establish busy.
func busySubmitInFixture(t *testing.T) (*Harness, *ConversationHandle, *SubmissionHandle, func()) {
	t.Helper()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		select {
		case <-entered:
		default:
			close(entered)
		}
		go func() {
			select {
			case <-release:
				ch <- terminal("answer")
			case <-ctx.Done():
			}
			close(ch)
		}()
		return ch
	})
	h := openHarness(t, store, options)
	t.Cleanup(unblock)
	c := root(t, h, ref)
	first, err := c.Submit(bg, Input{Type: "input", Content: "work"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	return h, c, first, unblock
}

// Literal harness-submit-in-commit.test.ts:209-230 assertion packet: the host's
// own document write, admission and later table write are one stored commit.
func TestSubmitInBusyHostCheckAndWritesAreOneCommit(t *testing.T) {
	h, c, first, _ := busySubmitInFixture(t)
	sent := mustDefinition(t, DefinitionOptions{Kind: "test.sent", Scope: "conversation", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"ids": []any{}}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
	before, observer, err := h.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Stop()
	frames := make(chan PublicationFrame, 1)
	if err = observer.Start(func(_ context.Context, frame PublicationFrame) error {
		for _, w := range frame.Publication.Tables {
			if w.Entry != nil && w.Entry.Kind == "app.sent" {
				frames <- frame
				break
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var id ID
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		live, err := builtin(tx, c.ID(), "pi.live")
		if err != nil {
			return nil, err
		}
		value, err := live.Get()
		if err != nil {
			return nil, err
		}
		if value["run"] == nil {
			return nil, errors.New("not working")
		}
		doc, err := tx.AcquireDocument(sent, c.ID(), nil, nil)
		if err != nil {
			return nil, err
		}
		id, err = c.SubmitIn(tx, Input{Type: "input", Content: "steer", WhenBusy: "steer"})
		if err != nil {
			return nil, err
		}
		if err = doc.Update(func(value JSON) error { value["ids"] = append(value["ids"].([]any), id); return nil }); err != nil {
			return nil, err
		}
		entry, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		return nil, tx.appendEntryRecord(Entry{ID: entry, Conversation: c.ID(), Kind: "app.sent", HasData: true, Data: JSON{"submission": id}})
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := h.Snapshot(bg)
	if err != nil || after.Seq != before.Seq+1 {
		t.Fatal("not one commit", before.Seq, after.Seq, err)
	}
	select {
	case frame := <-frames:
		foundSubmission, foundEntry := false, false
		for _, w := range frame.Publication.Tables {
			foundSubmission = foundSubmission || w.Submission != nil && w.Submission.ID == id
			foundEntry = foundEntry || w.Entry != nil && w.Entry.Kind == "app.sent"
		}
		if !foundSubmission || !foundEntry {
			t.Fatal("not stored together", frame.Publication)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stored frame missing")
	}
	value, found, err := h.SnapshotDefinition(bg, sent, c.ID(), nil)
	if err != nil || !found || !equalJSONValue(value.Copy(), JSON{"ids": []any{int(id)}}) {
		t.Fatal("host sent IDs", value, found, err)
	}
	inbox := mainContinueBoundaryDocument(t, after, c.ID(), "pi.inbox")
	if !equalJSONValue(inbox["items"], []any{JSON{"id": int(id), "mode": "steer", "content": "steer"}}) {
		t.Fatal("canonical inbox", inbox)
	}
	live := mainContinueBoundaryDocument(t, after, c.ID(), "pi.live")
	if !equalJSONValue(generationObject(live["run"])["inputs"], []any{int(first.ID())}) {
		t.Fatal("running inputs changed", live)
	}
}

// Source-derived from InboxItem and pickInputEntry (harness/inbox.ts:8-18,136).
// Assert the entire document value; no native-envelope acceptance transform.
func TestSubmitInInboxBlockContentAndEntryMetadata(t *testing.T) {
	h, c, _, _ := busySubmitInFixture(t)
	var id ID
	_, err := h.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		id, err = c.SubmitIn(tx, Input{Type: "input", WhenBusy: "steer", Blocks: []goai.ContentBlock{{Type: "text", Text: "hi"}}, Entry: &Entry{Kind: "app.ask", HasData: true, Data: nil}})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	inbox := mainContinueBoundaryDocument(t, state, c.ID(), "pi.inbox")
	want := JSON{"items": []any{JSON{"id": int(id), "mode": "steer", "content": []any{JSON{"type": "text", "text": "hi"}}, "entry": JSON{"kind": "app.ask", "data": nil}}}}
	if !equalJSONValue(inbox, want) {
		t.Fatal("canonical input inbox", inbox)
	}
}

// Source harness-submit-in-commit.test.ts:278-288: no Resume or Submission.Wait
// may supply the progress the admission itself must request. Use publication
// signals in place of the source's status polling.
func TestSubmitInIdleInputRequestsItsOwnProgress(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		ch <- terminal("answer")
		close(ch)
		return ch
	})
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	_, observer, err := h.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Stop()
	settled := make(chan SubmissionRecord, 1)
	if err = observer.Start(func(_ context.Context, frame PublicationFrame) error {
		for _, w := range frame.Publication.Tables {
			if w.Submission != nil && w.Submission.Type == "follow-up" && w.Submission.Status == "done" {
				settled <- publicSubmission(*w.Submission)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var id ID
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		id, err = c.SubmitIn(tx, Input{Type: "input", Content: "hi"})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-settled:
		if record.ID != id || record.Type != "input" || record.Status != "done" {
			t.Fatal(record, id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("admission did not request progress")
	}
}

// Source-derived from harness-submit-in-commit.test.ts:250-275. The SQLite
// seam reports a definite pre-append rollback; unknown I/O errors have separate
// poisoning contracts. No failure or reopened value is normalised by this test.
func TestSubmitInStorageFailureHasNoAdmissionAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "submission.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	h := openHarness(t, store, Options{})
	c := root(t, h, ModelRef{})
	failure := reject("disk full")
	appendFrame := store.appendFrame
	store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if kind == 2 {
			return failure
		}
		return appendFrame(kind, ordinal, high, payload)
	}
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		if _, err := c.SubmitIn(tx, Input{Type: "input", Content: "hi", RequestID: "r1"}); err != nil {
			return nil, err
		}
		id, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: c.ID(), Kind: "app.sent"})
	})
	if err != failure {
		t.Fatal("failure identity", err, failure)
	}
	deadline, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if end, err := h.session.Closed(deadline); err != nil || end.Reason != "failed" {
		t.Fatal(end, err)
	}
	// Session.closed has already closed its owned SQLite connection.
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	next := openHarness(t, reopened, Options{})
	conversation := root(t, next, ModelRef{})
	if conversation.ID() != c.ID() {
		t.Fatal("root changed")
	}
	_, err = next.Commit(bg, func(tx *Tx) (any, error) {
		record, found, err := tx.SubmissionByRequest(conversation.ID(), "r1")
		if err != nil || found {
			t.Fatal(record, found, err)
		}
		tasks, err := tx.ScanTasks(TaskQuery{Conversation: conversation.ID()}, 10, nil)
		if err != nil || len(tasks.Items) != 0 {
			t.Fatal(tasks, err)
		}
		entries, err := tx.ScanEntries(EntryQuery{Conversation: conversation.ID()}, 10, nil)
		if err != nil || len(entries.Items) != 0 {
			t.Fatal(entries, err)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	value, found, err := next.SnapshotDefinition(bg, mountedDocumentDefinitions[1], conversation.ID(), nil)
	if err != nil || !found || value.Copy().(map[string]any)["run"] != nil {
		t.Fatal("reopened run", value, found, err)
	}
}

// Source-derived from harness-submit-in-commit.test.ts:290-307: the provider
// rather than the source's tool is held at a boundary. Assert the canonical
// queued write draft as well as eventual placement and completion.
func TestSubmitInModelVisibleWriteQueueAndPlacement(t *testing.T) {
	h, c, first, release := busySubmitInFixture(t)
	note := userReceipt("a note the model sees")
	note.Timestamp = 1
	var id ID
	_, err := h.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		id, err = c.SubmitIn(tx, Input{Type: "write", Entry: &Entry{Kind: "test.note", Model: []MessageReceipt{note}}})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	inbox := mainContinueBoundaryDocument(t, state, c.ID(), "pi.inbox")
	want := JSON{"items": []any{JSON{"id": int(id), "mode": "write", "entry": JSON{"kind": "test.note", "model": []any{JSON{"role": "user", "content": []any{JSON{"type": "text", "text": "a note the model sees"}}, "timestamp": 1}}}}}}
	if !equalJSONValue(inbox, want) {
		t.Fatal("canonical queued write", inbox)
	}
	release()
	if result := waitSubmission(t, first); result.Record.Status != "done" {
		t.Fatal(result)
	}
	sub, err := h.Submission(bg, id)
	if err != nil || sub == nil {
		t.Fatal(sub, err)
	}
	record, err := sub.Status(bg)
	if err != nil || record.Type != "write" || record.Status != "done" || record.Entry == 0 {
		t.Fatal(record, err)
	}
	entry, found, err := h.Entry(bg, record.Entry)
	if err != nil || !found || entry.Kind != "test.note" || len(entry.Model) != 1 {
		t.Fatal(entry, found, err)
	}
}

// Literal harness-submit-in-commit.test.ts:367-384: a failed run can continue
// inside the caller transaction, but never after that transaction's table write.
func TestSubmitInContinuesFailedRunBeforeHostTableWrite(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if calls.Add(1) == 1 {
			message := terminal("").Message
			message.Content = nil
			message.StopReason = goai.StopReasonError
			message.ErrorMessage = "Invalid request"
			ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: message}
		} else {
			ch <- terminal("recovered")
		}
		close(ch)
		return ch
	})
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	first, err := c.Submit(bg, Input{Type: "input", Content: "Analyze this"})
	if err != nil {
		t.Fatal(err)
	}
	failed := waitSubmission(t, first)
	if failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" {
		t.Fatal(failed)
	}
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		id, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		if err = tx.appendEntryRecord(Entry{ID: id, Conversation: c.ID(), Kind: "app.note"}); err != nil {
			return nil, err
		}
		_, err = c.SubmitIn(tx, Input{Type: "continue"})
		return nil, err
	})
	var fence *ReadAfterWrite
	if !errors.As(err, &fence) {
		t.Fatal(err)
	}
	_, observer, err := h.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Stop()
	done := make(chan SubmissionRecord, 1)
	if err = observer.Start(func(_ context.Context, frame PublicationFrame) error {
		for _, w := range frame.Publication.Tables {
			if w.Submission != nil && w.Submission.Type == "continue" && w.Submission.Status == "done" {
				done <- publicSubmission(*w.Submission)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var continued ID
	_, err = h.Commit(bg, func(tx *Tx) (any, error) {
		var err error
		continued, err = c.SubmitIn(tx, Input{Type: "continue"})
		if err != nil {
			return nil, err
		}
		id, err := tx.MintID()
		if err != nil {
			return nil, err
		}
		return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: c.ID(), Kind: "app.note"})
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case record := <-done:
		if record.ID != continued || record.Type != "continue" || record.Status != "done" {
			t.Fatal(record)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("continue did not progress")
	}
}

// Source Session.commit<T>/Harness commit resolves with the callback's value,
// including arbitrary host identity; it must not substitute a commit sequence.
func TestSourcePublicCommitReturnsCallbackValue(t *testing.T) {
	type committer interface {
		Commit(context.Context, func(*Tx) (any, error), ...CommitOptions) (any, error)
	}
	for _, name := range []string{"session", "harness", "conversation"} {
		t.Run(name, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			var target any
			if name == "session" {
				session, err := OpenSession(store)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { session.Close(bg) })
				target = session
			} else {
				h := openHarness(t, store, Options{})
				conversation := root(t, h, ModelRef{Provider: "source", ID: "model"})
				target = h
				if name == "conversation" {
					target = conversation
				}
			}
			if _, exported := reflect.TypeOf(target).MethodByName("CommitTasks"); exported {
				t.Error("native alternate transaction entry remains public", name)
			}
			api, ok := target.(committer)
			if !ok {
				t.Fatal("source callback-value Commit signature missing", name)
			}
			marker := &struct {
				Cycle any
				Call  func() string
			}{Call: func() string { return "host identity" }}
			marker.Cycle = marker
			for _, value := range []any{nil, false, 0, "", "callback result", marker} {
				called := 0
				got, err := api.Commit(bg, func(*Tx) (any, error) { called++; return value, nil })
				if err != nil || got != value || called != 1 {
					t.Error("callback value replaced/copied or callback repeated", value, got, err, called)
				}
			}
			failure := errors.New("callback failure identity")
			got, err := api.Commit(bg, func(*Tx) (any, error) { return marker, failure })
			if got != nil || err != failure {
				t.Error("failed callback published a value or lost error identity", got, err)
			}
			got, err = api.Commit(bg, func(*Tx) (any, error) { return "still usable", nil })
			if got != "still usable" || err != nil {
				t.Error("callback error poisoned Session", got, err)
			}
		})
	}
}

// The entry assertions from session-tables.test.ts:136-189 and306-331 at
// f9a26365, through the public draft constructor. Conversation/task constructor
// assertions and source publication-object identity are not accepted here.
func TestSourceTxAppendEntryDraftReturnsRecord(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	var first, headed Entry
	value, err := session.Commit(bg, func(tx *Tx) (any, error) {
		appendEntry, ok := any(tx.AppendEntry).(func(ID, JSON) (Entry, error))
		if !ok {
			t.Fatal("source AppendEntry(conversation, draft) record-result signature missing")
		}
		var err error
		first, err = appendEntry(1, JSON{"kind": "note", "data": "one"})
		if err != nil {
			return nil, err
		}
		headed, err = appendEntry(1, JSON{"kind": "summary", "head": "self"})
		return headed, err
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == 0 || headed.ID == 0 || first.ID == headed.ID || headed.Head != headed.ID {
		t.Fatal("minted identities/self head", first, headed)
	}
	if first.Conversation != 1 || first.Kind != "note" || !first.HasData || first.Data != "one" || first.ByTask != 0 {
		t.Fatal("complete returned record", first)
	}
	firstWire, err := first.MarshalJSON()
	if err != nil || !reflect.DeepEqual(dropinJSON(t, firstWire), dropinJSON(t, []byte(fmt.Sprintf(`{"id":%d,"conversationId":1,"kind":"note","data":"one"}`, first.ID)))) {
		t.Fatal("source literal first record changed", string(firstWire), err)
	}
	if !reflect.DeepEqual(value, headed) {
		t.Fatal("commit replaced constructor record", value, headed)
	}
	for _, want := range []Entry{first, headed} {
		stored, found, err := store.Entry(bg, want.ID)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		gotWire, e := stored.Entry.MarshalJSON()
		if e != nil {
			t.Fatal(e)
		}
		wantWire, e := want.MarshalJSON()
		if e != nil || string(gotWire) != string(wantWire) {
			t.Fatal("stored/returned record differs", string(gotWire), string(wantWire), e)
		}
	}
}
func TestSourceTxAppendEntryTakesJSONOwnership(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	payload := JSON{"nested": JSON{"value": 1}}
	value, err := session.Commit(bg, func(tx *Tx) (any, error) {
		appendEntry, ok := any(tx.AppendEntry).(func(ID, JSON) (Entry, error))
		if !ok {
			t.Fatal("source draft constructor missing")
		}
		entry, err := appendEntry(1, JSON{"kind": "data", "data": payload})
		payload["nested"].(JSON)["value"] = 2
		return entry, err
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := value.(Entry)
	stored, found, err := store.Entry(bg, entry.ID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	if !reflect.DeepEqual(stored.Entry.Data, map[string]any{"nested": map[string]any{"value": json.Number("1")}}) {
		t.Fatal("caller mutation changed stored data", stored.Entry.Data)
	}
	for _, draft := range []JSON{{"kind": "omitted"}, {"kind": "null", "data": nil}, {"kind": "empty", "model": []any{}, "edits": []any{}}} {
		value, err = session.Commit(bg, func(tx *Tx) (any, error) { return any(tx.AppendEntry).(func(ID, JSON) (Entry, error))(1, draft) })
		if err != nil {
			t.Fatal(err)
		}
		got := value.(Entry)
		stored, found, err = store.Entry(bg, got.ID)
		if err != nil || !found {
			t.Fatal(found, err)
		}
		for _, record := range []Entry{got, stored.Entry} {
			wire, err := record.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			obj := dropinJSON(t, wire).(map[string]any)
			_, hasData := obj["data"]
			_, wantData := draft["data"]
			if hasData != wantData {
				t.Fatal("data presence changed", obj, draft)
			}
			if draft["kind"] == "empty" && (obj["model"] == nil || obj["edits"] == nil) {
				t.Fatal("explicit empty arrays omitted", obj)
			}
		}
	}
	before, err := store.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		return any(tx.AppendEntry).(func(ID, JSON) (Entry, error))(1, JSON{"kind": "invalid", "data": math.NaN()})
	})
	if err == nil || !strings.Contains(err.Error(), "strict JSON") {
		t.Fatal("source strict-JSON error changed", err)
	}
	after, err := store.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatal("invalid draft stored", before.Seq, after.Seq, err)
	}
}
func TestSourceTxAppendEntryMissingConversationBeforeMint(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		appendEntry, ok := any(tx.AppendEntry).(func(ID, JSON) (Entry, error))
		if !ok {
			t.Fatal("source draft constructor missing")
		}
		high := tx.state.HighWater
		_, err := appendEntry(12345, JSON{"kind": "note"})
		if err == nil || !strings.Contains(err.Error(), "Conversation 12345 does not exist") {
			t.Fatal("source missing-conversation error changed", err)
		}
		if tx.state.HighWater != high {
			t.Fatal("missing conversation minted an ID")
		}
		_, _, err = tx.Entry(1)
		var fenced *ReadAfterWrite
		if !errors.As(err, &fenced) {
			t.Fatal("failed entry write did not fence table reads", err)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// transaction.ts425-446 overrides identity/task fields before strict copying;
// EntryRecord.kind is any string, unlike defineEntry's non-empty token guard.
func TestSourceTxAppendEntryOverridesCallerMetadataAndAllowsEmptyKind(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	value, err := session.Commit(bg, func(tx *Tx) (any, error) {
		return tx.AppendEntry(1, JSON{"kind": "", "data": nil, "id": func() {}, "conversationId": math.NaN(), "byTaskId": func() {}})
	})
	if err != nil {
		t.Fatal("source overrides/empty string rejected", err)
	}
	entry := value.(Entry)
	if entry.ID == 0 || entry.Conversation != 1 || entry.ByTask != 0 || entry.Kind != "" || !entry.HasData || entry.Data != nil {
		t.Fatal(entry)
	}
	stored, found, err := store.Entry(bg, entry.ID)
	if err != nil || !found || stored.Entry.Kind != "" || !stored.Entry.HasData {
		t.Fatal(stored, found, err)
	}
}

// transaction.ts410-422's token overload replaces draft.kind before delegating
// to the same constructor; it does not accept the old native Value envelope.
func TestSourceTxAppendTypedEntryUsesDraftAndTokenKind(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	token := entryToken(t, "source.typed")
	value, err := session.Commit(bg, func(tx *Tx) (any, error) {
		appendEntry, ok := any(tx.AppendTypedEntry).(func(*EntryDefinition, ID, JSON) (Entry, error))
		if !ok {
			t.Fatal("source typed draft constructor missing")
		}
		return appendEntry(token, 1, JSON{"kind": "caller.kind", "data": nil, "head": "self", "model": []any{}, "edits": []any{}})
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := value.(Entry)
	if entry.ID == 0 || entry.Head != entry.ID || entry.Kind != token.kind || !entry.HasData || entry.Data != nil || entry.Model == nil || entry.Edits == nil {
		t.Fatal("token draft/record changed", entry)
	}
	stored, found, err := store.Entry(bg, entry.ID)
	if err != nil || !found || stored.Entry.Kind != token.kind {
		t.Fatal(stored, found, err)
	}
}

// Native SQLite persistence control for source-present empty arrays/null and
// the constructor's callback lifetime; source publication object identity is
// deliberately not inferred from detached Go record comparisons.
func TestSourceTxAppendEntryArraysNullAndLifetimeSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "entry-draft.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	token := entryToken(t, "source.reopen")
	var escaped *Tx
	value, err := session.Commit(bg, func(tx *Tx) (any, error) {
		escaped = tx
		return tx.AppendTypedEntry(token, 1, JSON{"data": nil, "head": "self", "model": []any{}, "edits": []any{}})
	})
	if err != nil {
		t.Fatal(err)
	}
	entry := value.(Entry)
	for _, call := range []func() (Entry, error){func() (Entry, error) { return escaped.AppendEntry(1, JSON{"kind": "late"}) }, func() (Entry, error) { return escaped.AppendTypedEntry(token, 1, JSON{}) }} {
		if _, err := call(); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped source constructor accepted", err)
		}
	}
	want, err := entry.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	stored, found, err := reopened.Entry(bg, entry.ID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	got, err := stored.Entry.MarshalJSON()
	if err != nil || string(got) != string(want) {
		t.Fatal("canonical constructor fields lost after reopen", string(got), string(want), err)
	}
}

// ContextEdit's replacement message array may be empty (types.ts441-453).
func TestSourceTxAppendEntryPreservesEmptyReplacementMessages(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	first, err := session.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(1, JSON{"kind": "note"}) })
	if err != nil {
		t.Fatal(err)
	}
	value, err := session.Commit(bg, func(tx *Tx) (any, error) {
		return tx.AppendEntry(1, JSON{"kind": "edit", "edits": []any{JSON{"target": first.(Entry).ID, "action": "replace", "messages": []any{}}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	record := value.(Entry)
	stored, found, err := store.Entry(bg, record.ID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	for _, entry := range []Entry{record, stored.Entry} {
		wire, err := entry.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		edit := dropinJSON(t, wire).(map[string]any)["edits"].([]any)[0].(map[string]any)
		messages, present := edit["messages"]
		if !present || !reflect.DeepEqual(messages, []any{}) {
			t.Fatal("source replacement array omitted", string(wire))
		}
	}
}

// Source Chord copyJson rejects cycles with this strict-JSON error. These are
// source-derived supplements, not additional original table-test assertions.
func TestSourceTxAppendEntryRejectsCyclesWithStrictJSONError(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	object := JSON{}
	object["self"] = object
	array := make([]any, 1)
	array[0] = array
	before, err := store.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range []any{object, array} {
		_, err = session.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(1, JSON{"kind": "invalid.cycle", "data": data}) })
		if err == nil || !strings.Contains(err.Error(), "Value contains cycles and is not strict JSON") {
			t.Error("source cycle rejection changed", err)
		}
	}
	after, err := store.Snapshot(bg)
	if err != nil || after.Seq != before.Seq {
		t.Fatal("cyclic draft reached storage", before.Seq, after.Seq, err)
	}
}

// Constructor field assertions from session-tables.test.ts136-181 at the
// pinned source. The original publication-object identity assertions remain a
// separate open packet; they are not replaced with value equality here.
func TestSourceTxCreateConversationOptionsAndConstructedRecords(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	idle := func(context.Context, TaskRecord, *TaskRuntime) error { return nil }
	work, err := DefineTask(TaskDefinitionOptions{Kind: "test.work", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "start"}, nil }, Phases: map[string]TaskPhase{"start": idle, "next": idle}, Abort: idle})
	if err != nil {
		t.Fatal(err)
	}
	var conversation Conversation
	var first, headed Entry
	var task ID
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		create, ok := any(tx.CreateConversation).(func(JSON) (Conversation, error))
		if !ok {
			t.Fatal("source CreateConversation(ownership options) record-result signature missing")
		}
		var err error
		conversation, err = create(JSON{"ownership": JSON{"kind": "ownerless"}})
		if err != nil {
			return nil, err
		}
		first, err = tx.AppendEntry(conversation.ID, JSON{"kind": "note", "data": "one"})
		if err != nil {
			return nil, err
		}
		headed, err = tx.AppendEntry(conversation.ID, JSON{"kind": "summary", "head": "self"})
		if err != nil {
			return nil, err
		}
		task, err = tx.CreateTask(work, JSON{"path": "x"}, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}, Conversation: conversation.ID, Background: true})
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[ID]bool{}
	for _, id := range []ID{conversation.ID, first.ID, headed.ID, task} {
		if id == 0 || ids[id] {
			t.Fatal("source constructor identities not distinct", conversation, first, headed, task)
		}
		ids[id] = true
	}
	if headed.Head != headed.ID {
		t.Fatal("source headed record changed", headed)
	}
	firstWire, err := first.MarshalJSON()
	if err != nil || !reflect.DeepEqual(dropinJSON(t, firstWire), dropinJSON(t, []byte(fmt.Sprintf(`{"id":%d,"conversationId":%d,"kind":"note","data":"one"}`, first.ID, conversation.ID)))) {
		t.Fatal("source first record changed", string(firstWire), err)
	}
	stored, found, err := store.Entry(bg, headed.ID)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	got, err := stored.Entry.MarshalJSON()
	want, e := headed.MarshalJSON()
	if err != nil || e != nil || string(got) != string(want) {
		t.Fatal("source stored headed record changed", string(got), string(want), err, e)
	}
	record, found, err := store.Task(bg, task)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	wire, err := record.MarshalJSON()
	expected := fmt.Sprintf(`{"id":%d,"conversationId":%d,"kind":"test.work","version":1,"input":{"path":"x"},"background":true,"abortRequested":false,"state":{"status":"pending","checkpoint":{"phase":"start"}}}`, task, conversation.ID)
	if err != nil || !reflect.DeepEqual(dropinJSON(t, wire), dropinJSON(t, []byte(expected))) {
		t.Fatal("source literal task record changed", string(wire), err)
	}
}

// Original session-tables.test.ts191-215 staged-owner record assertions, plus
// the exact missing-owner error from257. Identity/other atomic cases stay open.
func TestSourceTxCreateConversationStagedOwnerAndMissingOwner(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	idle := func(context.Context, TaskRecord, *TaskRuntime) error { return nil }
	work, err := DefineTask(TaskDefinitionOptions{Kind: "test.work", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "start"}, nil }, Phases: map[string]TaskPhase{"start": idle}, Abort: idle})
	if err != nil {
		t.Fatal(err)
	}
	var supervisor ID
	var child Conversation
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		create, ok := any(tx.CreateConversation).(func(JSON) (Conversation, error))
		if !ok {
			t.Fatal("source ownership-options constructor missing")
		}
		var err error
		supervisor, err = tx.CreateTask(work, JSON{"path": "background"}, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}, Background: true})
		if err != nil {
			return nil, err
		}
		child, err = create(JSON{"ownership": JSON{"kind": "task", "taskId": supervisor}})
		return child, err
	})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := child.MarshalJSON()
	want := fmt.Sprintf(`{"id":%d,"owner":{"conversationId":1,"taskId":%d}}`, child.ID, supervisor)
	if err != nil || !reflect.DeepEqual(dropinJSON(t, wire), dropinJSON(t, []byte(want))) {
		t.Fatal("staged owner record changed", string(wire), err)
	}
	stored, found, err := store.Conversation(bg, child.ID)
	if err != nil || !found || stored != child {
		t.Fatal("stored owner record changed", stored, found, err)
	}
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		create := any(tx.CreateConversation).(func(JSON) (Conversation, error))
		return create(JSON{"ownership": JSON{"kind": "task", "taskId": ID(999)}})
	})
	if err == nil || !strings.Contains(err.Error(), "Conversation owner task 999 does not exist") {
		t.Fatal("source missing-owner error changed", err)
	}
}

// Source-derived ownership-property evaluation, before allocation, confirmed by
// the original constructor probe. Unused option fields are never JSON-copied.
func TestSourceTxCreateConversationOwnershipEvaluation(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	for _, test := range []struct {
		options JSON
		message string
	}{{nil, "Cannot read properties of null (reading 'ownership')"}, {JSON{}, "Cannot read properties of undefined (reading 'kind')"}, {JSON{"ownership": nil}, "Cannot read properties of null (reading 'kind')"}} {
		_, err = session.Commit(bg, func(tx *Tx) (any, error) {
			before := tx.state.HighWater
			_, err := tx.CreateConversation(test.options)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Error("source option error changed", err)
			}
			if tx.state.HighWater != before {
				t.Error("option property error minted identity")
			}
			_, _, readErr := tx.Task(1)
			var fenced *ReadAfterWrite
			if !errors.As(readErr, &fenced) {
				t.Error("failed option write did not fence tables", readErr)
			}
			return nil, err
		})
		if err == nil {
			t.Fatal("invalid options stored")
		}
	}
	for _, ownership := range []any{JSON{"kind": "unknown", "ignored": func() {}}, true} {
		value, err := session.Commit(bg, func(tx *Tx) (any, error) {
			return tx.CreateConversation(JSON{"ownership": ownership, "ignored": func() {}})
		})
		if err != nil {
			t.Fatal("unused option fields validated", err)
		}
		record := value.(Conversation)
		wire, err := record.MarshalJSON()
		if err != nil || !reflect.DeepEqual(dropinJSON(t, wire), dropinJSON(t, []byte(fmt.Sprintf(`{"id":%d}`, record.ID)))) {
			t.Fatal("source ownerless record changed", string(wire), err)
		}
	}
}
func TestSourceTxCreateConversationRunsHarnessInitialization(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var created ID
	var note Entry
	h := openHarness(t, store, Options{ConversationCreated: func(tx *Tx, record Conversation) error {
		created = record.ID
		var err error
		note, err = tx.AppendEntry(record.ID, JSON{"kind": "source.created"})
		return err
	}})
	value, err := h.Commit(bg, func(tx *Tx) (any, error) { return tx.CreateConversation(JSON{"ownership": JSON{"kind": "ownerless"}}) })
	if err != nil {
		t.Fatal(err)
	}
	record := value.(Conversation)
	if record.ID == 0 || record.ID != created || note.Conversation != record.ID {
		t.Fatal("creation/initialization order", record, created, note)
	}
	stored, found, err := h.Entry(bg, note.ID)
	if err != nil || !found || stored.Kind != "source.created" {
		t.Fatal("creation callback write not adopted", stored, found, err)
	}
}

// transaction.ts370-377 tests the extracted taskId against undefined, not just
// ownership.kind. Omitted taskId is ownerless; explicit null remains a lookup.
func TestSourceTxCreateConversationOmittedVersusNullTaskOwner(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	value, err := session.Commit(bg, func(tx *Tx) (any, error) { return tx.CreateConversation(JSON{"ownership": JSON{"kind": "task"}}) })
	if err != nil {
		t.Error("source omitted taskId became owner lookup", err)
	} else {
		record := value.(Conversation)
		if record.ID == 0 || record.Owner != 0 || record.OwnerConversation != 0 {
			t.Error("omitted owner record changed", record)
		}
	}
	_, err = session.Commit(bg, func(tx *Tx) (any, error) {
		return tx.CreateConversation(JSON{"ownership": JSON{"kind": "task", "taskId": nil}})
	})
	if err == nil || !strings.Contains(err.Error(), "Conversation owner task null does not exist") {
		t.Error("source explicit-null lookup changed", err)
	}
}
