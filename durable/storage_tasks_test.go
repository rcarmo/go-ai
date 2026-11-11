package durable

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

// Source-derived raw Storage contract, independently run against pinned Memory
// and SQLite. No task definition may interpret or migrate stored input/state.
// Public Task and ScanTasks reads expose the source record directly; no test
// adapter translates native envelopes for these assertions.
func TestV150RawBuiltinStoragePreservesCanonicalRecords(t *testing.T) {
	raw, err := os.ReadFile("testdata/pi-durable-v150/builtin-storage-records.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Records []TaskRecord `json:"records"`
		Source  string       `json:"source"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "state.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close(bg) }()
	writes := make([]StorageWrite, len(fixture.Records))
	for i, record := range fixture.Records {
		writes[i] = StorageWrite{Type: "task", Value: record}
	}
	var receipts [][2]uint64
	result, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}, writes}, func(index int, seq uint64) { receipts = append(receipts, [2]uint64{uint64(index), seq}) })
	if err != nil || result.Stored != 2 || result.Refused != nil {
		t.Fatalf("raw builtin commit: %+v %v", result, err)
	}
	if !reflect.DeepEqual(receipts, [][2]uint64{{0, 1}, {1, 2}}) {
		t.Fatal(receipts)
	}
	check := func() {
		t.Helper()
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if state.Seq != 2 {
			t.Fatalf("read/reopen changed seq: %d", state.Seq)
		}
		for _, expected := range fixture.Records {
			actual, found, err := store.Task(bg, expected.ID)
			if err != nil || !found {
				t.Fatalf("canonical lookup found=%v err=%v", found, err)
			}
			want, err := expected.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			got, err := actual.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			var a, b any
			if err = json.Unmarshal(want, &a); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(got, &b); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Errorf("record%d got=%s want=%s", expected.ID, got, want)
			}
		}
		for _, status := range []string{"pending", "running", "waiting", "sleeping", "completing", "terminal"} {
			page, err := store.ScanTasks(bg, TaskQuery{Status: status}, 20, nil)
			if err != nil {
				t.Fatal(err)
			}
			var want, got []ID
			for _, record := range fixture.Records {
				if record.State.Status == status {
					want = append(want, record.ID)
				}
			}
			for _, record := range page.Items {
				got = append(got, record.ID)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("scan %s got=%v want=%v", status, got, want)
			}
		}
	}
	check()
	if err = store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	check()
	if err = store.Reclaim(bg); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	check()
}

// Native composition safety: newly admitted canonical records must never be
// interpreted as private checkpoints. Missing/future definitions stay blocked;
// builtin canonical execution/migration adapters remain a separate open port.
func TestV150CanonicalBuiltinStorageDoesNotEnterPrivateSchedulerAdapter(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}, {
		{Type: "task", Value: TaskRecord{ID: 2, Conversation: 1, Kind: "pi.generation", Version: 99, Input: &TaskValue{Present: true, Value: JSON{}}, State: TaskState{Status: "pending", Checkpoint: JSON{"phase": "prepare", "attempt": 1}}}},
		{Type: "task", Value: TaskRecord{ID: 3, Conversation: 1, Kind: "pi.tool", Version: 99, Input: &TaskValue{Present: true, Value: JSON{"kind": "model", "assistant": 900, "callId": "c1"}}, State: TaskState{Status: "pending", Checkpoint: JSON{"phase": "call"}}}},
	}}, nil)
	if err != nil || result.Refused != nil {
		t.Fatalf("admission: %+v %v", result, err)
	}
	h := openHarness(t, store, Options{})
	before, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	reservations, err := h.scheduler.reservePass(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 0 {
		t.Fatal("canonical builtins were reserved without definitions")
	}
	after, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if after.Seq != before.Seq {
		t.Fatal("canonical blocked work was reinterpreted or stored")
	}
	for _, id := range []ID{2, 3} {
		actual, ok, err := h.Task(bg, id)
		if err != nil || !ok || actual.State.Status != "pending" {
			t.Fatalf("record%d: %+v %v", id, actual, err)
		}
	}
}

// Expected values come directly from original pi-durable1.5 MemoryStorage,
// with no Go-side field aliases, wrapper removal or presence normalization.
func dropinTaskFixtures(t *testing.T) []struct {
	Name   string
	Record json.RawMessage
} {
	t.Helper()
	data, err := os.ReadFile("testdata/pi-durable-v150/task-records.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Target   string
		Package  string
		Version  string
		Fixtures []struct {
			Name   string
			Record json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if source.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" || source.Package != "@earendil-works/pi-durable" || source.Version != "1.5.0" || len(source.Fixtures) != 13 {
		t.Fatal("wrong original fixture provenance")
	}
	return source.Fixtures
}

func dropinJSON(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPiDurableV150TaskRecordWireRoundTrip(t *testing.T) {
	for _, fixture := range dropinTaskFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			var record TaskRecord
			if err := json.Unmarshal(fixture.Record, &record); err != nil {
				t.Fatalf("valid original record rejected: %v", err)
			}
			got, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, fixture.Record)) {
				t.Fatalf("original record changed\ngot  %s\nwant %s", got, fixture.Record)
			}
		})
	}
}

func TestPiDurableV150TaskStateWireRoundTrip(t *testing.T) {
	for _, fixture := range dropinTaskFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(fixture.Record, &raw); err != nil {
				t.Fatal(err)
			}
			var state TaskState
			if err := json.Unmarshal(raw["state"], &state); err != nil {
				t.Fatalf("valid original state rejected: %v", err)
			}
			got, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, raw["state"])) {
				t.Fatalf("original state changed\ngot  %s\nwant %s", got, raw["state"])
			}
		})
	}
}

// A fresh canonical task JSON value must not be reinterpreted as the native
// private presence wrapper, even when the value has identical-looking keys.
func TestPiDurableV150TaskWireKeepsWrapperShapedUserData(t *testing.T) {
	raw := []byte(`{"id":2,"conversationId":1,"kind":"compat.task","version":1,"input":{"present":true,"value":"user"},"background":false,"abortRequested":false,"state":{"status":"terminal","outcome":{"status":"completed","result":{"present":true,"value":"result"}}}}`)
	var record TaskRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, raw)) {
		t.Fatalf("wrapper-shaped user data changed: got=%s want=%s", got, raw)
	}
}

// A real private execution envelope must remain readable using both the
// bounded storage codec and standard Snapshot JSON used by crash witnesses.
func TestPiDurableV150PrivateTaskEnvelopeStillRoundTrips(t *testing.T) {
	original := foundationTask(2, JSON{"present": true, "value": "user"})
	original.Status = "done"
	original.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"present": true, "value": "result"}}}}
	native, err := encodeBounded(original, DefaultLimits(), DefaultLimits().MaxRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	normal, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{native, normal} {
		var got Task
		if err := decodeStrict(raw, DefaultLimits(), DefaultLimits().MaxRecordBytes, &got); err != nil {
			t.Fatalf("private record decode: %v", err)
		}
		if !equalTaskValue(got.Execution.Native.Input, original.Execution.Native.Input, DefaultLimits()) || !equalTaskValue(got.Execution.Native.State.Outcome.Result, original.Execution.Native.State.Outcome.Result, DefaultLimits()) {
			t.Fatalf("private record was reinterpreted: %s", raw)
		}
	}
}

func TestPiDurableV150PrivateLargeTaskEnvelopeWithinHardLimits(t *testing.T) {
	// The persisted maximum is 1 MiB, higher than the default 512 KiB. Standard
	// crash-witness serialization cannot reject data accepted by that policy.
	limits := DefaultLimits()
	limits.MaxRecordBytes = 1 << 20
	limits.MaxStringBytes = 512 << 10
	original := foundationTask(2, JSON{"a": strings.Repeat("a", 300<<10), "b": strings.Repeat("b", 300<<10)})
	if _, err := copyTask(original, limits); err != nil {
		t.Fatalf("valid larger persisted task rejected: %v", err)
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("valid larger crash witness rejected: %v", err)
	}
	var got Task
	if err := decodeStrict(raw, limits, limits.MaxRecordBytes, &got); err != nil {
		t.Fatalf("larger private record decode: %v", err)
	}
	if !equalTaskValue(got.Execution.Native.Input, original.Execution.Native.Input, limits) {
		t.Fatal("larger private input changed")
	}
}
