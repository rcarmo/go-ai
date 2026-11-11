package durable

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

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
