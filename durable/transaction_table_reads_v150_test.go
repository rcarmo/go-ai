package durable

import (
	"testing"
)

// Literal read-before-first-table-write contract from session-tables.test.ts:85
// at f9a26365. No staged-table read contract is carried forward.
func TestTxV150TaskAndTableReadsBeforeFirstTableWrite(t *testing.T) {
	store := taskConformanceStoreV150(t)
	task := conformanceTaskV150(mint(t, store))
	storageTasksCommitV150(t, store, task)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	_, err = session.Commit(bg, func(tx *Tx) error {
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
