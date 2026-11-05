package durable

import (
	"context"
	"encoding/json"
	"testing"
)

// Original TaskRecord/Storage keeps opaque JsonValue checkpoints until the
// registered definition migrates them. Reading an old record must not require
// the current phase object's shape, and migration must receive the full value.
func TestPiDurableV150OpaqueCheckpointMigration(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var seen any
	definition, err := DefineTask(TaskDefinitionOptions{Kind: "compat.opaque-checkpoint", Version: 2, Initial: func(any) (JSON, error) { return JSON{"phase": "work"}, nil }, Migrate: func(input any, checkpoint any, version uint64) (any, JSON, error) {
		seen = checkpoint
		if version != 1 || !equalJSONValue(checkpoint, json.Number("42")) {
			t.Errorf("opaque migration input changed: version=%d value=%v", version, checkpoint)
		}
		return input, JSON{"phase": "work"}, nil
	}, Phases: map[string]TaskPhase{"work": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("migrated"), nil })
	}}, Abort: taskAbort})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	record := Task{ID: id, Conversation: 1, Kind: definition.Kind(), Status: "pending", Checkpoint: JSON{}, Execution: &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{Version: 1, Input: &TaskValue{Present: true, Value: nil}, State: TaskState{Status: "pending", Checkpoint: json.Number("42")}}}}
	if _, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &record}}}); err != nil {
		t.Fatalf("valid original opaque record rejected: %v", err)
	}
	harness := taskTestHarness(t, store, definition)
	before, found, err := harness.Task(bg, id)
	if err != nil || !found || !equalJSONValue(before.State.Checkpoint, json.Number("42")) {
		t.Fatalf("opaque stored record changed before scheduling: %+v %v", before, err)
	}
	result := waitPublicTask(t, harness, id)
	if seen == nil || result.Version != 2 || result.State.Outcome.Status != "completed" || result.State.Outcome.Result.Value != "migrated" {
		t.Fatalf("migration result changed: seen=%v result=%+v", seen, result)
	}
}

// Storage is not the task-definition validator: preserve a future/opaque
// checkpoint on read. With no migration, a missing phase faults the actual
// invocation rather than panicking or dispatching a different handler.
func TestPiDurableV150UnknownPhaseFaultsOnlyOnExecution(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	definition := taskDefinition(t, "compat.unknown-phase", func(context.Context, TaskRecord, *TaskRuntime) error { calls++; return nil })
	id, err := store.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	record := Task{ID: id, Conversation: 1, Kind: definition.Kind(), Status: "pending", Checkpoint: JSON{}, Execution: &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{Version: 1, Input: &TaskValue{Present: true, Value: nil}, State: TaskState{Status: "pending", Checkpoint: JSON{"phase": 1}}}}}
	if _, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &record}}}); err != nil {
		t.Fatalf("original-shaped stored task rejected: %v", err)
	}
	harness := taskTestHarness(t, store, definition)
	before, found, err := harness.Task(bg, id)
	if err != nil || !found || !equalJSONValue(taskCheckpointMap(before.State.Checkpoint)["phase"], 1) {
		t.Fatalf("opaque checkpoint lost before dispatch: %+v %v", before, err)
	}
	result := waitPublicTask(t, harness, id)
	if calls != 0 || result.State.Outcome == nil || result.State.Outcome.Status != "faulted" {
		t.Fatalf("unknown phase dispatched or did not fault: calls=%d result=%+v", calls, result)
	}
}
