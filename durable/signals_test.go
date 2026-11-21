package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

// Official v1.6.0 harness-signals.test.ts:84, defineSignal.
func TestSourceV160DefineSignal(t *testing.T) {
	str := JSON{"type": "string"}
	obj := func(props JSON) JSON { return JSON{"type": "object", "properties": props} }
	for _, tc := range []struct {
		definition SignalDefinition
		message    string
	}{
		{SignalDefinition{Name: "a/b", Version: 1, Schema: str}, `Signal name "a/b" must be non-empty, without "/"`},
		{SignalDefinition{Name: "a", Version: 0, Schema: str}, "Signal a version must be a positive integer"},
		{SignalDefinition{Name: "a", Version: 1, Schema: obj(JSON{"n": JSON{"type": "bigint"}})}, "Signal a schema admits non-JSON values (bigint)"},
		{SignalDefinition{Name: "a", Version: 1, Schema: obj(JSON{"b": JSON{"type": "bytes"}})}, "Signal a schema admits non-JSON values (bytes)"},
		{SignalDefinition{Name: "a", Version: 1, Schema: JSON{"type": "object", "dependencies": JSON{"a": []any{"b"}, "b": obj(JSON{"n": JSON{"type": "bigint"}})}}}, "Signal a schema admits non-JSON values (bigint)"},
	} {
		if token, err := DefineSignal(tc.definition); err == nil || err.Error() != tc.message || token != nil {
			t.Errorf("DefineSignal(%+v) = %v, %v; want %q", tc.definition, token, err, tc.message)
		}
	}
	annotation := obj(JSON{"type": str})
	annotation["examples"] = []any{JSON{"type": "bigint"}}
	if _, err := DefineSignal(SignalDefinition{Name: "a", Version: 1, Schema: annotation}); err != nil {
		t.Fatalf("annotations are not schema nodes: %v", err)
	}
	answer, err := DefineSignal(SignalDefinition{Name: "test.answer", Version: 1, Schema: obj(JSON{"text": str})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := answer.Key(""); err == nil || err.Error() != "Signal test.answer key must be non-empty" {
		t.Fatalf("empty signal key: %v", err)
	}
	if key, err := answer.Key("k"); err != nil || key != (SignalKey{Signal: "test.answer", Key: "k"}) {
		t.Fatalf("signal key = %+v, %v", key, err)
	}
}

// Supplemental delivery probe for the data-only part of
// harness-signals.test.ts:102. A held runtime phase supplies a stable live task;
// signal-triggered wake is checked separately when signal waits are enabled.
func TestV160SignalDeliveryStoresOnlyTheWinningJSONValue(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	entered, finish := make(chan struct{}), make(chan struct{})
	def := taskDefinition(t, "test.signal.delivery", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		close(entered)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-finish:
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true}}}, nil
		})
	})
	h := taskTestHarness(t, store, def)
	cleanupTaskGates(t, finish)
	v, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := v.(ID)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-entered:
	}
	definition := func(name string) *SignalToken {
		t.Helper()
		token, err := DefineSignal(SignalDefinition{Name: name, Version: 1, Schema: JSON{
			"type": "object", "properties": JSON{"text": JSON{"type": "string"}}, "required": []any{"text"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	answer := definition("test.answer")
	open := definition("test.open")
	deliver := func(task ID, token *SignalToken, key string, value any, want string) {
		t.Helper()
		got, err := h.Signal(ctx, task, token, key, value)
		if string(got) != want || err != nil {
			t.Fatalf("signal(%d, %s/%s, %v) = %q, %v; want %q", task, token.Name(), key, value, got, err, want)
		}
	}
	deliver(999999, answer, "a", JSON{"text": "x"}, "not_found")
	deliver(id, answer, "a", JSON{"text": 1}, "invalid")
	deliver(id, open, "a", JSON{"text": "x", "n": math.NaN()}, "invalid")
	payload := JSON{"text": "yes"}
	deliver(id, answer, "a", payload, "delivered")
	payload["text"] = "changed"
	deliver(id, answer, "a", JSON{"text": "again"}, "duplicate")
	record, found, err := h.Task(ctx, id)
	if want := map[string]*TaskValue{"pi.signal.test.answer/a": {Present: true, Value: map[string]any{"version": json.Number("1"), "value": map[string]any{"text": "yes"}}}}; err != nil || !found || !reflect.DeepEqual(record.Memos, want) {
		t.Fatalf("stored memos = %#v, want %#v; found=%v, err=%v", record.Memos, want, found, err)
	}
	releaseTaskGate(finish)
	terminal, err := h.WaitForTask(ctx, id)
	if err != nil || terminal.State.Outcome == nil || terminal.State.Outcome.Status != "completed" {
		t.Fatalf("task ended with %+v, %v", terminal, err)
	}
	deliver(id, answer, "b", JSON{"text": "late"}, "closed")
}

// Supplemental half of the v1.6.0 self-signal case: the task's own sleeping
// transition must not overwrite the memo staged in the same commit.
func TestV160SignalComposesWithOwnTaskState(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	answer, err := DefineSignal(SignalDefinition{Name: "test.answer", Version: 1, Schema: JSON{"type": "object", "properties": JSON{"text": JSON{"type": "string"}}, "required": []any{"text"}}})
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	def := taskDefinition(t, "test.self", func(run context.Context, _ TaskRecord, r *TaskRuntime) error {
		until, err := r.Now()
		if err != nil {
			finished <- err
			return err
		}
		until += 60_000
		err = r.Commit(run, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			result, err := tx.Signal(r.TaskID(), answer, "self", JSON{"text": "me"})
			if err != nil {
				return nil, err
			}
			if result != SignalDelivered {
				return nil, fmt.Errorf("signal result = %q", result)
			}
			return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "work"}, Until: &until}, nil
		})
		finished <- err
		return err
	})
	h := taskTestHarness(t, store, def)
	value, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	id := value.(ID)
	record, found, err := h.Task(ctx, id)
	if err != nil || !found || record.State.Status != "sleeping" {
		t.Fatalf("task after own transition: %+v, %v, %v", record, found, err)
	}
	want := map[string]*TaskValue{"pi.signal.test.answer/self": {Present: true, Value: map[string]any{"version": json.Number("1"), "value": map[string]any{"text": "me"}}}}
	if !reflect.DeepEqual(record.Memos, want) {
		t.Fatalf("sleeping task memos = %#v, want %#v", record.Memos, want)
	}
}
