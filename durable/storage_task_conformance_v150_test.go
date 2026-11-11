package durable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Literal task cases from src/testing/storage-conformance.ts:671,723,759 at
// f9a26365. Public lookup/scans must expose TaskRecord without native envelopes.
func conformanceTaskV150(id ID) TaskRecord {
	return TaskRecord{ID: id, Conversation: 1, Kind: "test.task", Version: 1, Input: &TaskValue{Present: true, Value: JSON{"value": id}}, State: TaskState{Status: "pending", Checkpoint: JSON{"phase": "ready"}}}
}

func requireStorageTaskV150(t *testing.T, s Storage, id ID) TaskRecord {
	t.Helper()
	record, found, err := s.Task(bg, id)
	if err != nil || !found {
		t.Fatalf("task%d found=%v err=%v", id, found, err)
	}
	return record
}

func requireStorageTaskEqualV150(t *testing.T, actual, expected TaskRecord) {
	t.Helper()
	a, err := actual.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := expected.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var av, bv any
	if err = json.Unmarshal(a, &av); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &bv); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(av, bv) {
		t.Errorf("record got=%s want=%s", a, b)
	}
}

func storageTasksCommitV150(t *testing.T, s Storage, records ...TaskRecord) {
	t.Helper()
	writes := make([]StorageWrite, len(records))
	for i, r := range records {
		writes[i] = StorageWrite{Type: "task", Value: r}
	}
	result, err := s.Commit(bg, [][]StorageWrite{writes}, nil)
	if err != nil || result.Refused != nil || result.Stored != 1 {
		t.Fatalf("task commit: %+v %v", result, err)
	}
}

func taskConformanceStoreV150(t *testing.T) *SQLiteStorage {
	t.Helper()
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "conformance.db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(bg) })
	result, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}}, nil)
	if err != nil || result.Refused != nil {
		t.Fatal(result, err)
	}
	return s
}

func scanConformanceTasksV150(t *testing.T, s Storage, q TaskQuery, limit int, cursor Cursor) ([]TaskRecord, Cursor) {
	t.Helper()
	page, err := s.ScanTasks(bg, q, limit, cursor)
	if err != nil {
		t.Fatal(err)
	}
	return page.Items, page.Next
}

func TestStorageV150ReplacesCompleteTaskRecordsAndPagesFilteredScans(t *testing.T) {
	s := taskConformanceStoreV150(t)
	ids := []ID{mint(t, s), mint(t, s), mint(t, s)}
	first, second, third := conformanceTaskV150(ids[0]), conformanceTaskV150(ids[1]), conformanceTaskV150(ids[2])
	first.Memos = map[string]*TaskValue{"winner": {Present: true, Value: "first"}}
	second.Background = true
	third.AbortRequested = true
	storageTasksCommitV150(t, s, first, second, third)
	running := first
	running.State = TaskState{Status: "running", Checkpoint: JSON{"phase": "effect", "attempt": 1}}
	running.AbortRequested = true
	storageTasksCommitV150(t, s, running)
	requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, ids[0]), running)
	terminal := conformanceTaskV150(ids[0])
	terminal.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"entryId": 99}}}}
	terminal.AbortRequested = true
	storageTasksCommitV150(t, s, terminal)
	requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, ids[0]), terminal)
	page, next := scanConformanceTasksV150(t, s, TaskQuery{Status: "pending"}, 1, nil)
	if len(page) != 1 || page[0].ID != ids[1] || next == nil {
		t.Fatal(page, next)
	}
	page, _ = scanConformanceTasksV150(t, s, TaskQuery{Status: "pending"}, 1, next)
	if len(page) != 1 || page[0].ID != ids[2] {
		t.Fatal(page)
	}
	yes := true
	page, _ = scanConformanceTasksV150(t, s, TaskQuery{Status: "terminal", AbortRequested: &yes}, 10, nil)
	if len(page) != 1 {
		t.Fatal(page)
	}
	requireStorageTaskEqualV150(t, page[0], terminal)
	page, _ = scanConformanceTasksV150(t, s, TaskQuery{Background: &yes}, 10, nil)
	if len(page) != 1 || page[0].ID != ids[1] {
		t.Fatal(page)
	}
}

func TestStorageV150StoresOwnersAndScansWaitingCompletingStates(t *testing.T) {
	s := taskConformanceStoreV150(t)
	owner, waiting, completing := conformanceTaskV150(mint(t, s)), conformanceTaskV150(mint(t, s)), conformanceTaskV150(mint(t, s))
	waiting.Owner = owner.ID
	waiting.State = TaskState{Status: "waiting", Checkpoint: JSON{"phase": "next"}, On: []ID{owner.ID}, Policy: "allSettled"}
	waiting.Memos = map[string]*TaskValue{"kept": {Present: true, Value: true}}
	completing.Owner = owner.ID
	completing.State = TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "failed", Error: &TaskOutcomeError{Message: "held"}}}
	storageTasksCommitV150(t, s, owner, waiting, completing)
	requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, waiting.ID), waiting)
	requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, completing.ID), completing)
	for _, r := range []TaskRecord{waiting, completing, owner} {
		page, _ := scanConformanceTasksV150(t, s, TaskQuery{Status: r.State.Status}, 10, nil)
		if len(page) != 1 {
			t.Fatal(page)
		}
		requireStorageTaskEqualV150(t, page[0], r)
	}
	terminal := completing
	terminal.State.Status = "terminal"
	storageTasksCommitV150(t, s, terminal)
	page, _ := scanConformanceTasksV150(t, s, TaskQuery{Status: "completing"}, 10, nil)
	if len(page) != 0 {
		t.Fatal(page)
	}
	page, _ = scanConformanceTasksV150(t, s, TaskQuery{Status: "terminal"}, 10, nil)
	if len(page) != 1 {
		t.Fatal(page)
	}
	requireStorageTaskEqualV150(t, page[0], terminal)
}

func TestStorageV150StoresAndScansSleepingTasks(t *testing.T) {
	s := taskConformanceStoreV150(t)
	sleeping := conformanceTaskV150(mint(t, s))
	until := int64(61000)
	sleeping.State = TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "next"}, Until: &until}
	sleeping.Memos = map[string]*TaskValue{"kept": {Present: true, Value: true}}
	storageTasksCommitV150(t, s, sleeping)
	requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, sleeping.ID), sleeping)
	page, _ := scanConformanceTasksV150(t, s, TaskQuery{Status: "sleeping"}, 10, nil)
	if len(page) != 1 {
		t.Fatal(page)
	}
	requireStorageTaskEqualV150(t, page[0], sleeping)
	running := sleeping
	running.State = TaskState{Status: "running", Checkpoint: JSON{"phase": "next"}}
	storageTasksCommitV150(t, s, running)
	page, _ = scanConformanceTasksV150(t, s, TaskQuery{Status: "sleeping"}, 10, nil)
	if len(page) != 0 {
		t.Fatal(page)
	}
	page, _ = scanConformanceTasksV150(t, s, TaskQuery{Status: "running"}, 10, nil)
	if len(page) != 1 {
		t.Fatal(page)
	}
	requireStorageTaskEqualV150(t, page[0], running)
}

// Source-derived extension of complete-record replacement. Original raw stores
// do not enforce the Session's frozen identity, marks, outcome or lifecycle.
func TestStorageV150RawTaskReplacementDoesNotApplySessionRules(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/task-storage-replacements.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Records []TaskRecord }
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "replace.db")
	s, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close(bg) }()
	owner := conformanceTaskV150(3)
	result, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}, {Type: "conversation", Value: Conversation{ID: 2}}, {Type: "task", Value: owner}}}, nil)
	if err != nil || result.Refused != nil {
		t.Fatal(result, err)
	}
	for _, record := range fixture.Records {
		storageTasksCommitV150(t, s, record)
		requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, 4), record)
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
		s, err = OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, 4), record)
		if err = s.Reclaim(bg); err != nil {
			t.Fatal(err)
		}
		if err = s.Close(bg); err != nil {
			t.Fatal(err)
		}
		s, err = OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		requireStorageTaskEqualV150(t, requireStorageTaskV150(t, s, 4), record)
	}
}
