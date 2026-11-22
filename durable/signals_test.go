package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
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

// Official v1.6.0 harness-signals.test.ts:463. The same commit delivers a
// signal to its own task and parks it without a deadline. The reserved phase
// receives the exact persisted wake reason.
func TestSourceV160SelfSignalWakesWithExactReason(t *testing.T) {
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
	key, err := answer.Key("self")
	if err != nil {
		t.Fatal(err)
	}
	def, err := DefineTask(TaskDefinitionOptions{Kind: "test.self.wake", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "start"}, nil }, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}, Phases: map[string]TaskPhase{
		"start": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				got, err := tx.Signal(r.TaskID(), answer, "self", JSON{"text": "me"})
				if err != nil {
					return nil, err
				}
				if got != SignalDelivered {
					return nil, fmt.Errorf("self signal = %q", got)
				}
				return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "woke"}, Signals: []SignalKey{key}}, nil
			})
		},
		"woke": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			raw, err := json.Marshal(task.State.Wake)
			if err != nil {
				return err
			}
			var wake any
			if err = json.Unmarshal(raw, &wake); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(wake), nil })
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, store, def)
	v, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.WaitForTask(ctx, v.(ID))
	if err != nil {
		t.Fatal(err)
	}
	want := &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: map[string]any{"reason": "signal", "signal": "test.answer", "key": "self"}}}
	if !reflect.DeepEqual(task.State.Outcome, want) {
		t.Fatalf("self signal outcome = %+v, want %+v", task.State.Outcome, want)
	}
}

func testSignalAnswer(t *testing.T) *SignalToken {
	t.Helper()
	token, err := DefineSignal(SignalDefinition{Name: "test.answer", Version: 1, Schema: JSON{
		"type": "object", "properties": JSON{"text": JSON{"type": "string"}}, "required": []any{"text"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

// v1.6.0 harness-signals.test.ts:158: a value delivered before the sleep
// is visible when the task wakes and is returned in the exact outcome.
func TestSourceV160SignalDeliveredBeforeSleep(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	answer := testSignalAnswer(t)
	def := signalAskDefinition(t, answer, nil, nil)
	h := taskTestHarness(t, store, def)
	value, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, JSON{"keys": []any{"a"}}, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := value.(ID)
	if got, err := h.Signal(ctx, id, answer, "a", JSON{"text": "early"}); got != SignalDelivered || err != nil {
		t.Fatalf("early signal = %q, %v", got, err)
	}
	task, err := h.WaitForTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: map[string]any{
		"wake":   map[string]any{"reason": "signal", "signal": "test.answer", "key": "a"},
		"states": []any{map[string]any{"value": map[string]any{"text": "early"}}},
	}}}
	if !reflect.DeepEqual(task.State.Outcome, want) {
		got, _ := json.Marshal(task.State.Outcome)
		expected, _ := json.Marshal(want)
		t.Fatalf("early signal outcome = %s, want %s", got, expected)
	}
}

// Official v1.6.0 harness-signals.test.ts:145. Two sequential deliveries
// are a read-after-write, so the entire callback must be discarded.
func TestSourceV160SignalReadAfterWrite(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	definition := taskDefinition(t, "test.signal.order", func(context.Context, TaskRecord, *TaskRuntime) error {
		return errors.New("paused task was invoked")
	})
	h := taskTestHarness(t, store, definition)
	value, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(definition, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := value.(ID)
	answer := testSignalAnswer(t)
	_, err = h.Commit(ctx, func(tx *Tx) (any, error) {
		if got, err := tx.Signal(id, answer, "a", JSON{"text": "one"}); got != SignalDelivered || err != nil {
			return nil, fmt.Errorf("first signal = %q, %v", got, err)
		}
		return tx.Signal(id, answer, "b", JSON{"text": "two"})
	})
	var afterWrite *ReadAfterWrite
	if !errors.As(err, &afterWrite) || afterWrite.Method != "signal" {
		t.Fatalf("sequential signal = %v; want ReadAfterWrite for signal", err)
	}
	if task, found, err := h.Task(ctx, id); err != nil || !found || task.Memos != nil {
		t.Fatalf("rejected signal commit stored memos: %#v (%v, %v)", task.Memos, found, err)
	}
}

func signalAskDefinition(t *testing.T, answer *SignalToken, until *int64, parked chan<- error, names ...string) *TaskDefinition {
	t.Helper()
	if len(names) == 0 {
		names = []string{"a"}
	}
	keys := make([]SignalKey, len(names))
	for i, name := range names {
		key, err := answer.Key(name)
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = key
	}
	def, err := DefineTask(TaskDefinitionOptions{Kind: "test.ask", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "ask"}, nil },
		Abort:   abortTestSignalTask,
		Phases: map[string]TaskPhase{
			"ask": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "answered"}, Until: until, Signals: keys}, nil
				})
				if parked != nil {
					parked <- err
				}
				return err
			},
			"answered": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				states := make([]*SignalState, 0, len(names))
				for _, name := range names {
					state, err := r.ReadSignal(ctx, answer, name)
					if err != nil {
						return err
					}
					states = append(states, state)
				}
				raw, err := json.Marshal(JSON{"wake": task.State.Wake, "states": states})
				if err != nil {
					return err
				}
				var result any
				if err = json.Unmarshal(raw, &result); err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(result), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return def
}

// v1.6.0 harness-signals.test.ts:225. Advance the injected clock inside the
// delivery's public commit so reservation cannot run between the clock change
// and the delivery. No timer or wall-clock delay establishes this order.
func TestSourceV160LateSignalExpiresBeforeReservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	var clock atomic.Int64
	clock.Store(1000)
	until := int64(2000)
	parked := make(chan error, 1)
	answer := testSignalAnswer(t)
	def := signalAskDefinition(t, answer, &until, parked)
	h := taskTestHarnessOptions(t, store, Options{Now: clock.Load, OnWake: func(Wake) {}}, def)
	value, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, JSON{"keys": []any{"a"}, "until": until}, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := value.(ID)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-parked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	result, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		clock.Store(2500)
		return tx.Signal(id, answer, "a", JSON{"text": "late"})
	})
	if err != nil || result != SignalClosed {
		t.Fatalf("late signal = %v, %v; want closed", result, err)
	}
	record, err := h.WaitForTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: map[string]any{
		"wake": map[string]any{"reason": "expired"}, "states": []any{map[string]any{"expired": true}},
	}}}
	if !reflect.DeepEqual(record.State.Outcome, want) {
		got, _ := json.Marshal(record.State.Outcome)
		expected, _ := json.Marshal(want)
		t.Fatalf("late signal outcome = %s, want %s", got, expected)
	}
}

// v1.6.0 harness-signals.test.ts:627: a same-checkpoint commit preserves
// the wake in running and pending SQLite records until checkpoint progress.
func TestSourceV160SignalWakeSurvivesSameCheckpointAndReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	answer := testSignalAnswer(t)
	key, err := answer.Key("k")
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan *WakeReason, 5)
	var runs atomic.Int32
	def, err := DefineTask(TaskDefinitionOptions{Kind: "test.keep", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "sleep"}, nil },
		Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "test"}}, nil
			})
		},
		Phases: map[string]TaskPhase{
			"sleep": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "woke"}, Signals: []SignalKey{key}}, nil
				})
			},
			"woke": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				seen <- task.State.Wake
				if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "woke"}}, nil
				}); err != nil {
					return err
				}
				after, found, err := r.Task(ctx, r.TaskID())
				if err != nil {
					return err
				}
				if !found {
					return errors.New("own live task missing")
				}
				seen <- after.State.Wake
				if runs.Add(1) == 1 {
					<-ctx.Done()
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "after"}}, nil
				})
			},
			"after": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				after, found, err := r.Task(ctx, r.TaskID())
				if err != nil {
					return err
				}
				if !found {
					return errors.New("own live task missing")
				}
				seen <- after.State.Wake
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, store, def)
	v, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := v.(ID)
	if result, err := h.Signal(ctx, id, answer, "k", JSON{"text": "go"}); result != SignalDelivered || err != nil {
		t.Fatalf("signal = %v, %v", result, err)
	}
	wake := &WakeReason{Reason: "signal", Signal: "test.answer", Key: "k"}
	expectSeen := func(want *WakeReason) {
		t.Helper()
		select {
		case got := <-seen:
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("phase saw wake %+v, want %+v", got, want)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	expectState := func(h *Harness, status string) {
		t.Helper()
		task, found, err := h.Task(ctx, id)
		if err != nil || !found {
			t.Fatalf("task state unavailable: %v, %v", found, err)
		}
		raw, err := json.Marshal(task.State)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"status": status, "checkpoint": map[string]any{"phase": "woke"}, "wake": map[string]any{"reason": "signal", "signal": "test.answer", "key": "k"}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("task state = %s; want %+v", raw, want)
		}
	}
	expectSeen(wake)
	expectSeen(wake)
	expectState(h, "running")
	if err = h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := taskTestHarness(t, reopenStoreAfterHarnessClose(t, store), def)
	expectState(reopened, "pending")
	if err = reopened.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if task, err := reopened.WaitForTask(ctx, id); err != nil || !reflect.DeepEqual(task.State.Outcome, taskDone(nil).Outcome) {
		t.Fatalf("resumed outcome = %+v, %v", task.State.Outcome, err)
	}
	expectSeen(wake)
	expectSeen(wake)
	expectSeen(nil)
	if len(seen) != 0 {
		t.Fatalf("unexpected extra wake observations: %d", len(seen))
	}
}

// Official v1.6.0 harness-signals.test.ts:525: parent waits for any child,
// then aborts the remaining child and returns precisely the ended IDs.
func TestSourceV160WaitAnyResumesWithEndedTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 6*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	answer := testSignalAnswer(t)
	childParked := make(chan error, 2)
	child := signalAskDefinition(t, answer, nil, childParked)
	parentParked := make(chan error, 1)
	parent := signalWaitAnyDefinition(t, child, parentParked)
	h := taskTestHarness(t, store, child, parent)
	id, first, second := startSignalWaitAny(t, ctx, h, parent, parentParked, childParked)
	if got, err := h.Signal(ctx, second, answer, "a", JSON{"text": "done"}); got != SignalDelivered || err != nil {
		t.Fatalf("child signal = %v, %v", got, err)
	}
	finished, err := h.WaitForTask(ctx, id)
	want := taskDone(map[string]any{"reason": "tasks", "ended": []any{json.Number(idText(second))}}).Outcome
	if err != nil || !reflect.DeepEqual(finished.State.Outcome, want) {
		got, _ := json.Marshal(finished.State.Outcome)
		expected, _ := json.Marshal(want)
		t.Fatalf("parent outcome = %s, %v; want %s", got, err, expected)
	}
	if child, found, err := h.Task(ctx, first); err != nil || !found || child.State.Outcome == nil || child.State.Outcome.Status != "aborted" {
		t.Fatalf("first child outcome = %+v, %v", child.State.Outcome, err)
	}
}

func signalWaitAnyDefinition(t *testing.T, child *TaskDefinition, parentParked chan<- error) *TaskDefinition {
	t.Helper()
	parent, err := DefineTask(TaskDefinitionOptions{Kind: "test.any", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil },
		Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "test"}}, nil
			})
		},
		Phases: map[string]TaskPhase{
			"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					options := TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}}
					on := make([]ID, 2)
					for i := range on {
						id, err := tx.CreateTask(child, JSON{"keys": []any{"a"}}, options)
						if err != nil {
							return nil, err
						}
						on[i] = id
					}
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "joined", "on": on}, On: on, Policy: "any"}, nil
				})
				parentParked <- err
				return err
			},
			"joined": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				members, _ := taskCheckpointMap(task.State.Checkpoint)["on"].([]any)
				if len(members) != 2 {
					return fmt.Errorf("joined checkpoint members = %v", members)
				}
				for _, id := range members {
					if err := r.AbortOwned(ctx, submissionEntryID(id)); err != nil {
						return err
					}
				}
				raw, err := json.Marshal(task.State.Wake)
				if err != nil {
					return err
				}
				var value any
				decoder := json.NewDecoder(strings.NewReader(string(raw)))
				decoder.UseNumber()
				if err := decoder.Decode(&value); err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(value), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return parent
}

func startSignalWaitAny(t *testing.T, ctx context.Context, h *Harness, parent *TaskDefinition, parentParked, childParked <-chan error) (id, first, second ID) {
	t.Helper()
	value, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(parent, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id = value.(ID)
	if err = h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	await := func(c <-chan error) {
		t.Helper()
		select {
		case err := <-c:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	await(parentParked)
	await(childParked)
	await(childParked)
	task, found, err := h.Task(ctx, id)
	if err != nil || !found || task.State.Status != "waiting" || len(task.State.On) != 2 {
		t.Fatalf("parent did not wait: %+v, %v", task.State, err)
	}
	return id, task.State.On[0], task.State.On[1]
}

// v1.6.0 harness-signals.test.ts:535. With child definitions unavailable
// after reopen, a direct abort orphans one child without enabling scheduling.
// Only the parent then contributes active work to Wake.
func TestSourceV160WaitAnyWakeActivatesWhilePaused(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	childParked, parentParked := make(chan error, 2), make(chan error, 1)
	child := signalAskDefinition(t, testSignalAnswer(t), nil, childParked)
	parent := signalWaitAnyDefinition(t, child, parentParked)
	h := taskTestHarness(t, store, child, parent)
	id, first, _ := startSignalWaitAny(t, ctx, h, parent, parentParked, childParked)
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h = taskTestHarnessOptions(t, reopenStoreAfterHarnessClose(t, store), Options{OnWake: func(Wake) {}}, parent)
	checkWake := func(active bool) {
		t.Helper()
		wake, err := h.Wake(ctx)
		if err != nil || wake != (Wake{Active: active}) {
			t.Fatalf("wake = %+v, %v; want only active=%v", wake, err, active)
		}
		inspection, err := h.Inspect(ctx)
		if err != nil || inspection.Scheduling != "paused" {
			t.Fatalf("scheduling = %s, %v; want paused", inspection.Scheduling, err)
		}
	}
	checkWake(false)
	if result, err := h.AbortTask(ctx, first); err != nil || result != "marked" {
		t.Fatalf("blocked child abort = %q, %v", result, err)
	}
	if record, found, err := h.Task(ctx, first); err != nil || !found || record.State.Status != "terminal" {
		t.Fatalf("blocked child = %+v, %v, %v", record.State, found, err)
	}
	checkWake(true)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	record, err := h.WaitForTask(ctx, id)
	want := taskDone(map[string]any{"reason": "tasks", "ended": []any{json.Number(idText(first))}}).Outcome
	if err != nil || !reflect.DeepEqual(record.State.Outcome, want) {
		got, _ := json.Marshal(record.State.Outcome)
		expected, _ := json.Marshal(want)
		t.Fatalf("parent outcome = %s, %v; want %s", got, err, expected)
	}
}

// v1.6.0 harness-signals.test.ts:244: consumed keys cannot be reused, and
// the runtime reports the task ID rather than a schema-level sleep diagnostic.
func TestSourceV160SignalSleepRejectsConsumedKeyAndEmptyWait(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	answer := testSignalAnswer(t)
	key, err := answer.Key("a")
	if err != nil {
		t.Fatal(err)
	}
	rejections := make(chan error, 1)
	def, err := DefineTask(TaskDefinitionOptions{
		Kind: "test.twice", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "first"}, nil },
		Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "test"}}, nil
			})
		},
		Phases: map[string]TaskPhase{
			"first": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					next := &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "second"}}
					if generationObject(task.Input.Value)["signals"] == true {
						next.Signals = []SignalKey{key}
					}
					return next, nil
				})
			},
			"second": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "first"}, Signals: []SignalKey{key}}, nil
				})
				rejections <- err
				if err == nil {
					return errors.New("consumed key accepted")
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, store, def)
	start := func(signals bool) ID {
		t.Helper()
		v, err := h.Commit(ctx, func(tx *Tx) (any, error) {
			return tx.CreateTask(def, JSON{"signals": signals}, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
		})
		if err != nil {
			t.Fatal(err)
		}
		return v.(ID)
	}
	consumed := start(true)
	if result, err := h.Signal(ctx, consumed, answer, "a", JSON{"text": "once"}); err != nil || result != SignalDelivered {
		t.Fatalf("signal = %q, %v", result, err)
	}
	select {
	case err := <-rejections:
		want := fmt.Sprintf("Task %d cannot sleep on signal test.answer key a: it already woke a wait", consumed)
		if err == nil || err.Error() != want {
			t.Fatalf("resleep = %v; want %q", err, want)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if task, err := h.WaitForTask(ctx, consumed); err != nil || !reflect.DeepEqual(task.State.Outcome, taskDone(nil).Outcome) {
		t.Fatalf("completed after rejection: %+v, %v", task.State.Outcome, err)
	}
	empty := start(false)
	receipt, err := h.WaitForTask(ctx, empty)
	want := fmt.Sprintf("Task %d cannot sleep without until or signals", empty)
	if got := receipt.State.Outcome; err != nil || got == nil || got.Status != "faulted" || got.Error == nil || got.Error.Message != want {
		t.Fatalf("empty wait = %+v, %v; want fault %q", got, err, want)
	}
}

// Supplemental check of a signal-ready sleep with a future deadline. Reopen
// without running phases, deliver in a non-resuming commit, then read Wake.
func TestV160WakePrefersSignalToFutureDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 8*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	answer := testSignalAnswer(t)
	until := int64(2000)
	parked := make(chan error, 1)
	def := signalAskDefinition(t, answer, &until, parked)
	options := Options{Now: func() int64 { return 1000 }, OnWake: func(Wake) {}}
	h := taskTestHarnessOptions(t, store, options, def)
	v, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, JSON{"keys": []any{"a"}, "until": until}, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := v.(ID)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-parked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h = taskTestHarnessOptions(t, reopenStoreAfterHarnessClose(t, store), options, def)
	before, err := h.Wake(ctx)
	if err != nil || before.Active || before.At == nil || *before.At != until {
		t.Fatalf("before signal = %+v, %v", before, err)
	}
	result, err := h.Commit(ctx, func(tx *Tx) (any, error) { return tx.Signal(id, answer, "a", JSON{"text": "later"}) })
	if err != nil || result != SignalDelivered {
		t.Fatalf("signal = %v, %v", result, err)
	}
	after, err := h.Wake(ctx)
	if err != nil || after != (Wake{Active: true}) {
		t.Fatalf("after signal = %+v, %v", after, err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	record, err := h.WaitForTask(ctx, id)
	want := taskDone(map[string]any{"wake": map[string]any{"reason": "signal", "signal": "test.answer", "key": "a"}, "states": []any{map[string]any{"value": map[string]any{"text": "later"}}}}).Outcome
	if err != nil || !reflect.DeepEqual(record.State.Outcome, want) {
		got, _ := json.Marshal(record.State.Outcome)
		expected, _ := json.Marshal(want)
		t.Fatalf("result = %s, %v; want %s", got, err, expected)
	}
}

type signalCommitRecorder struct {
	Storage
	mu      sync.Mutex
	batches [][]StorageWrite
}

func (s *signalCommitRecorder) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	return s.Storage.Commit(ctx, batches, func(i int, seq uint64) {
		s.mu.Lock()
		s.batches = append(s.batches, append([]StorageWrite(nil), batches[i]...))
		s.mu.Unlock()
		if receive != nil {
			receive(i, seq)
		}
	})
}

func (s *signalCommitRecorder) requireNotified(t *testing.T, waiter ID, expected map[ID]string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, batch := range s.batches {
		settled := map[ID]SubmissionRecord{}
		var taskWrites []TaskRecord
		for _, write := range batch {
			switch write.Type {
			case "submission":
				if record, ok := recordValue[SubmissionRecord](write.Value); ok && expected[record.ID] != "" && record.Status == "done" {
					settled[record.ID] = record
				}
			case "task":
				if task, ok := recordValue[TaskRecord](write.Value); ok && task.ID == waiter {
					taskWrites = append(taskWrites, task)
				}
			}
		}
		if len(settled) == 0 {
			continue
		}
		if len(settled) != len(expected) || len(taskWrites) != 1 {
			t.Fatalf("settling batch: %d submissions, %d waiter writes", len(settled), len(taskWrites))
		}
		if len(expected) == 2 {
			var keys []string
			for key := range taskWrites[0].Memos {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			want := []string{"pi.signal.pi.submission-settled/one", "pi.signal.pi.submission-settled/two"}
			if !slices.Equal(keys, want) {
				t.Fatalf("paired settlement memo keys = %v; want %v", keys, want)
			}
		}
		for id, key := range expected {
			memo := taskWrites[0].Memos[signalMemoName(SubmissionSettled.Name(), key)]
			if memo == nil || !memo.Present {
				t.Fatalf("batch did not write key %s", key)
			}
			stored := generationObject(memo.Value)
			got, err := json.Marshal(stored["value"])
			if err != nil {
				t.Fatal(err)
			}
			want, err := json.Marshal(settled[id])
			if err != nil {
				t.Fatal(err)
			}
			var actualValue, expectedValue any
			if decodePublicJSON(got, &actualValue) != nil || decodePublicJSON(want, &expectedValue) != nil || !reflect.DeepEqual(actualValue, expectedValue) {
				t.Fatalf("key %s: payload %s, want exact settled record %s", key, got, want)
			}
		}
		return
	}
	t.Fatal("no batch stored the settled submissions")
}

// v1.6.0 harness-park-children.test.ts, submissions that notify a task.
// The recorder witnesses settlement and notification in the same batch.
func TestSourceV160SubmissionNotifications(t *testing.T) {
	for _, name := range []string{"input-settlement", "created-settled", "paired-queued-writes", "invalid-payload", "missing-target"} {
		t.Run(name, func(t *testing.T) {
			queued := name == "paired-queued-writes"
			blocked := name == "created-settled"
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			native, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			storage := &signalCommitRecorder{Storage: native}
			entered, release, parked := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			keys := []string{"settled"}
			if queued {
				keys = []string{"one", "two"}
			}
			signals := make([]SignalKey, 0, len(keys))
			for _, key := range keys {
				sk, err := SubmissionSettled.Key(key)
				if err != nil {
					t.Fatal(err)
				}
				signals = append(signals, sk)
			}
			ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				close(entered)
				go func() {
					defer close(ch)
					select {
					case <-release:
						ch <- terminal("answer")
					case <-ctx.Done():
					}
				}()
				return ch
			})
			waiterDef, err := DefineTask(TaskDefinitionOptions{Kind: "test.notify.waiter", Version: 1,
				Initial: func(any) (JSON, error) { return JSON{"phase": "sleep"}, nil },
				Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "test"}}, nil
					})
				},
				Phases: map[string]TaskPhase{
					"sleep": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
							return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "woke"}, Signals: signals}, nil
						})
						parked <- err
						return err
					},
					"woke": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
						var value any
						if !queued {
							state, err := r.ReadSignal(ctx, SubmissionSettled, "settled")
							if err != nil {
								return err
							}
							if state != nil {
								value = generationObject(state.Value)["status"]
							}
						}
						return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(value), nil })
					},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			options.Registry = NewRegistry()
			if !blocked {
				if _, err := options.Registry.RegisterTask(waiterDef); err != nil {
					t.Fatal(err)
				}
			}
			h := openHarness(t, storage, options)
			defer releaseTaskGate(release)
			c := root(t, h, ref)
			if name == "missing-target" {
				sub, err := c.Submit(ctx, Input{Type: "input", Content: "go", Notify: &SubmissionNotify{TaskID: 999999, Key: "settled"}})
				if err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(release)
				if settled, err := sub.Wait(ctx); err != nil || settled.Record.Status != "done" {
					t.Fatalf("settled without target = %+v, %v", settled.Record, err)
				}
				return
			}
			value, err := c.Commit(ctx, func(tx *Tx) (any, error) {
				return tx.CreateTask(waiterDef, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			})
			if err != nil {
				t.Fatal(err)
			}
			waiter := value.(ID)
			if name == "invalid-payload" {
				for _, value := range []any{
					17,
					JSON{"id": 2, "conversationId": 1, "type": "input", "status": "done"},
					JSON{"id": 2, "conversationId": 1, "type": "input", "status": "unanswered", "reason": "x", "entry": "e1"},
				} {
					if result, err := h.Signal(ctx, waiter, SubmissionSettled, "settled", value); err != nil || result != SignalInvalid {
						t.Fatalf("invalid payload %v = %q, %v", value, result, err)
					}
				}
				if _, err := h.AbortTask(ctx, waiter); err != nil {
					t.Fatal(err)
				}
				return
			}
			var run *SubmissionHandle
			if !blocked {
				if err := h.Resume(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-parked:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if queued {
				run, err = c.Submit(ctx, Input{Type: "input", Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			expected := map[ID]string{}
			var submissions []*SubmissionHandle
			for _, key := range keys {
				entry := &Entry{Kind: "test.note", Data: "hi", HasData: true}
				if queued {
					entry.Model = []MessageReceipt{userReceipt(key)}
				}
				notify := &SubmissionNotify{TaskID: waiter, Key: key}
				draft := Input{Type: "write", Entry: entry, Notify: notify}
				if name == "input-settlement" {
					draft = Input{Type: "input", Content: "go", Notify: notify}
				}
				sub, err := c.Submit(ctx, draft)
				if err != nil {
					t.Fatal(err)
				}
				status, err := sub.Status(ctx)
				want := "done"
				if queued {
					want = "queued"
				} else if !blocked {
					want = "placed"
				}
				if err != nil || status.Status != want {
					t.Fatalf("submission status = %s, %v; want %s", status.Status, err, want)
				}
				expected[sub.ID()] = key
				submissions = append(submissions, sub)
			}
			var result any = "done"
			if !blocked {
				if !queued {
					select {
					case <-entered:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				releaseTaskGate(release)
				if run != nil {
					if _, err := run.Wait(ctx); err != nil {
						t.Fatal(err)
					}
					result = nil
				}
			} else {
				if task, found, err := h.Task(ctx, waiter); err != nil || !found || task.StartedAt != nil {
					t.Fatalf("waiter started before notification: %+v, %v", task, err)
				}
				if _, err := options.Registry.RegisterTask(waiterDef); err != nil {
					t.Fatal(err)
				}
			}
			for _, sub := range submissions {
				if settled, err := sub.Wait(ctx); err != nil || settled.Record.Status != "done" {
					t.Fatalf("settled = %+v, %v", settled.Record, err)
				}
			}
			if got, err := h.WaitForTask(ctx, waiter); err != nil || !reflect.DeepEqual(got.State.Outcome, taskDone(result).Outcome) {
				t.Fatalf("waiter outcome = %+v, %v", got.State.Outcome, err)
			}
			storage.requireNotified(t, waiter, expected)
		})
	}
}

func TestSourceV160ParallelSignalsComposeDeliveriesInOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 6*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	answer := testSignalAnswer(t)
	def := signalAskDefinition(t, answer, nil, nil, "a", "b")
	h := taskTestHarness(t, store, def)
	v, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(def, JSON{"keys": []any{"a", "b"}}, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := v.(ID)
	v, err = h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.Signals(SignalDelivery{id, answer, "a", JSON{"text": "first"}}, SignalDelivery{id, answer, "a", JSON{"text": "second"}}, SignalDelivery{id, answer, "b", JSON{"text": "bee"}})
	})
	if err != nil || !reflect.DeepEqual(v, []SignalResult{SignalDelivered, SignalDuplicate, SignalDelivered}) {
		t.Fatalf("deliveries = %v, %v", v, err)
	}
	got, err := h.WaitForTask(ctx, id)
	want := taskDone(map[string]any{"wake": map[string]any{"reason": "signal", "signal": "test.answer", "key": "a"}, "states": []any{map[string]any{"value": map[string]any{"text": "first"}}, map[string]any{"value": map[string]any{"text": "bee"}}}}).Outcome
	if err != nil || !reflect.DeepEqual(got.State.Outcome, want) {
		actual, _ := json.Marshal(got.State.Outcome)
		expected, _ := json.Marshal(want)
		t.Fatalf("outcome = %s, %v; want %s", actual, err, expected)
	}
}

func TestSourceV160SignalReadingVersionsAndMigration(t *testing.T) {
	for _, name := range []string{"newer", "older-no-migration", "migrated", "invalid-migration"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 6*time.Second)
			defer cancel()
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			define := func(version uint64, kind string, migrate func(any, uint64) (any, error)) *SignalToken {
				t.Helper()
				token, err := DefineSignal(SignalDefinition{Name: "test.versioned", Version: version, Schema: JSON{"type": "object", "properties": JSON{"n": JSON{"type": kind}}, "required": []any{"n"}}, Migrate: migrate})
				if err != nil {
					t.Fatal(err)
				}
				return token
			}
			var deliver, read *SignalToken
			var payload any
			wantError := ""
			switch name {
			case "newer":
				deliver, read, payload = define(2, "integer", nil), define(1, "integer", nil), JSON{"n": 2}
				wantError = "Signal test.versioned value has version 2, newer than 1"
			case "older-no-migration":
				deliver, read, payload = define(1, "integer", nil), define(2, "integer", nil), JSON{"n": 1}
				wantError = "Signal test.versioned has no migration from version 1"
			default:
				deliver, payload = define(1, "string", nil), JSON{"n": "42"}
				read = define(2, "integer", func(value any, version uint64) (any, error) {
					if version != 1 || !reflect.DeepEqual(value, map[string]any{"n": "42"}) {
						return nil, fmt.Errorf("migration saw %v, %v", value, version)
					}
					if name == "invalid-migration" {
						return value, nil
					}
					return JSON{"n": 42}, nil
				})
				if name == "invalid-migration" {
					wantError = "Signal test.versioned migration from version 1 returned an invalid value"
				}
			}
			proceed := make(chan struct{})
			results := make(chan struct {
				State *SignalState
				Err   error
			}, 1)
			def := taskDefinition(t, "test.read-versions", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-proceed:
				}
				state, err := r.ReadSignal(ctx, read, "k")
				results <- struct {
					State *SignalState
					Err   error
				}{state, err}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			h := taskTestHarness(t, store, def)
			defer releaseTaskGate(proceed)
			v, err := h.Commit(ctx, func(tx *Tx) (any, error) {
				return tx.CreateTask(def, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
			})
			if err != nil {
				t.Fatal(err)
			}
			id := v.(ID)
			if got, err := h.Signal(ctx, id, deliver, "k", payload); got != SignalDelivered || err != nil {
				t.Fatalf("deliver = %q, %v", got, err)
			}
			releaseTaskGate(proceed)
			if _, err := h.WaitForTask(ctx, id); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-results:
				if wantError != "" {
					if got.Err == nil || got.Err.Error() != wantError || got.State != nil {
						t.Fatalf("read = %+v; want %q", got, wantError)
					}
				} else {
					raw, err := json.Marshal(got.State)
					if got.Err != nil || err != nil || string(raw) != `{"value":{"n":42}}` {
						t.Fatalf("read = %s, %v, %v", raw, got.Err, err)
					}
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

// Source harness-signals.test.ts:177. The graph exposes the ordered waiting
// keys without retaining an invocation or lending the watcher its slices.
func TestSourceV160SignalSleepGraph(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	answer := testSignalAnswer(t)
	parked := make(chan error, 1)
	wakes := make(chan Wake, 16)
	def := signalAskDefinition(t, answer, nil, parked, "a", "b")
	h := taskTestHarnessOptions(t, store, Options{OnWake: func(w Wake) {
		select {
		case wakes <- w:
		default:
		}
	}}, def)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	watch, err := h.WatchTaskGraph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Stop()
	sleeping := make(chan TaskGraphState, 1)
	if err := watch.Start(func(_ context.Context, graph TaskGraph, _ []Operation) error {
		if state := graph.Tasks[idText(id)].State; state.Status == "sleeping" {
			select {
			case sleeping <- state:
			default:
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-parked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for {
		wake, err := h.Wake(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !wake.Active {
			if wake != (Wake{Active: false}) {
				t.Fatalf("sleep without a deadline: %+v", wake)
			}
			break
		}
		select {
		case <-wakes:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	const wanted = `{"status":"sleeping","phase":"answered","signals":[{"signal":"test.answer","key":"a"},{"signal":"test.answer","key":"b"}]}`
	var observed TaskGraphState
	select {
	case observed = <-sleeping:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	data, err := json.Marshal(observed)
	if err != nil || string(data) != wanted {
		t.Fatalf("sleep graph state = %s, %v; want %s", data, err, wanted)
	}
	snapshot := watch.Value()
	snapshot.Tasks[idText(id)].State.Signals[0].Key = "changed snapshot"
	observed.Signals[1].Key = "changed callback"
	data, err = json.Marshal(watch.Value().Tasks[idText(id)].State)
	if err != nil || string(data) != wanted {
		t.Fatalf("detached sleep graph state = %s, %v; want %s", data, err, wanted)
	}
	inspection, err := h.Inspect(ctx)
	if err != nil || len(inspection.Tasks) != 1 || inspection.Tasks[0].Record.ID != id || inspection.Tasks[0].Kind != "sleeping" {
		t.Fatalf("sleep inspection = %+v, %v", inspection, err)
	}
	if got, err := h.Signal(ctx, id, answer, "b", JSON{"text": "bee"}); got != SignalDelivered || err != nil {
		t.Fatalf("signal b = %q, %v", got, err)
	}
	record, err := h.WaitForTask(ctx, id)
	want := taskDone(map[string]any{
		"wake":   map[string]any{"reason": "signal", "signal": "test.answer", "key": "b"},
		"states": []any{nil, map[string]any{"value": map[string]any{"text": "bee"}}},
	}).Outcome
	if err != nil || !reflect.DeepEqual(record.State.Outcome, want) {
		got, _ := json.Marshal(record.State.Outcome)
		t.Fatalf("sleep result %s, %v", got, err)
	}
}

// Source harness-signals.test.ts:207, :342, :387 and :556, using an injected clock
// and committed sleep receipts rather than waiting for a wall-clock timer.
func TestSourceV160SignalSleepEndings(t *testing.T) {
	for _, ending := range []string{"deadline", "resleep-expired", "abort", "reopen"} {
		t.Run(ending, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			var clock atomic.Int64
			clock.Store(1000)
			deadline := int64(2000)
			var until *int64
			if ending == "deadline" || ending == "resleep-expired" {
				until = &deadline
			}
			wakes := make(chan Wake, 16)
			options := Options{Now: clock.Load, OnWake: func(w Wake) {
				select {
				case wakes <- w:
				default:
				}
			}}
			answer := testSignalAnswer(t)
			parked := make(chan error, 1)
			def := signalAskDefinition(t, answer, until, parked)
			if ending == "resleep-expired" {
				key, err := answer.Key("a")
				if err != nil {
					t.Fatal(err)
				}
				def.options.Phases["answered"] = func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "ask"}, Signals: []SignalKey{key}}, nil
					})
					if err == nil {
						return errors.New("accepted an expired signal key")
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone([]any{err.Error()}), nil })
				}
			}
			h := taskTestHarnessOptions(t, store, options, def)
			id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-parked:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			for {
				wake, err := h.Wake(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if !wake.Active {
					if !sameWake(wake, Wake{Active: false, At: until}) {
						t.Fatalf("sleep wake = %+v; want inactive with deadline %v", wake, until)
					}
					break
				}
				select {
				case <-wakes:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			var want *TaskOutcome
			switch ending {
			case "deadline", "resleep-expired":
				clock.Store(deadline)
				if err := h.Resume(ctx); err != nil {
					t.Fatal(err)
				}
				if ending == "resleep-expired" {
					want = taskDone([]any{fmt.Sprintf("Task %d cannot sleep on signal test.answer key a: it expired", id)}).Outcome
				} else {
					want = taskDone(map[string]any{"wake": map[string]any{"reason": "expired"}, "states": []any{map[string]any{"expired": true}}}).Outcome
				}
			case "abort":
				if got, err := h.AbortTask(ctx, id); err != nil || got != "marked" {
					t.Fatalf("abort = %q, %v", got, err)
				}
				want = &TaskOutcome{Status: "aborted", Reason: "test"}
			case "reopen":
				if err := h.Close(ctx); err != nil {
					t.Fatal(err)
				}
				h = taskTestHarnessOptions(t, reopenStoreAfterHarnessClose(t, store), options, def)
				if wake, err := h.Wake(ctx); err != nil || wake != (Wake{Active: false}) {
					t.Fatalf("reopened sleep wake = %+v, %v", wake, err)
				}
				got, err := h.Commit(ctx, func(tx *Tx) (any, error) { return tx.Signal(id, answer, "a", JSON{"text": "later"}) })
				if err != nil || got != SignalDelivered {
					t.Fatalf("reopened sleep delivery = %v, %v", got, err)
				}
				if wake, err := h.Wake(ctx); err != nil || wake != (Wake{Active: true}) {
					t.Fatalf("reopened wake after delivery = %+v, %v", wake, err)
				}
				if inspection, err := h.Inspect(ctx); err != nil || inspection.Scheduling != "paused" {
					t.Fatalf("delivery enabled scheduling: %+v, %v", inspection, err)
				}
				if err := h.Resume(ctx); err != nil {
					t.Fatal(err)
				}
				want = taskDone(map[string]any{"wake": map[string]any{"reason": "signal", "signal": "test.answer", "key": "a"}, "states": []any{map[string]any{"value": map[string]any{"text": "later"}}}}).Outcome
			}
			record, err := h.WaitForTask(ctx, id)
			actual, encodeErr := json.Marshal(record.State.Outcome)
			expected, _ := json.Marshal(want)
			if err != nil || encodeErr != nil || string(actual) != string(expected) {
				t.Fatalf("ending outcome = %s, %v, %v; want %s", actual, err, encodeErr, expected)
			}
		})
	}
}

func abortTestSignalTask(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
	return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
		return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "test"}}, nil
	})
}

func TestSourceV160SignalConsumesKeysInSleepOrder(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	answer := testSignalAnswer(t)
	keyA, _ := answer.Key("a")
	keyB, _ := answer.Key("b")
	var seen []any
	observe := func(wake *WakeReason) {
		if wake == nil {
			seen = append(seen, nil)
			return
		}
		seen = append(seen, map[string]any{"reason": wake.Reason, "signal": wake.Signal, "key": wake.Key})
	}
	def, err := DefineTask(TaskDefinitionOptions{Kind: "test.two", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "first"}, nil },
		Abort:   abortTestSignalTask,
		Phases: map[string]TaskPhase{
			"first": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "second"}, Signals: []SignalKey{keyA, keyB}}, nil
				})
			},
			"second": func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
				observe(record.State.Wake)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "done"}, Signals: []SignalKey{keyB}}, nil
				})
			},
			"done": func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
				observe(record.State.Wake)
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(seen), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarness(t, store, def)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	for _, signal := range []struct{ key, value string }{{"b", "bee"}, {"a", "ay"}} {
		got, err := h.Commit(ctx, func(tx *Tx) (any, error) { return tx.Signal(id, answer, signal.key, JSON{"text": signal.value}) })
		if err != nil || got != SignalDelivered {
			t.Fatalf("paused delivery %q: %v, %v", signal.key, got, err)
		}
	}
	result, err := h.WaitForTask(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := taskDone([]any{
		map[string]any{"reason": "signal", "signal": "test.answer", "key": "a"},
		map[string]any{"reason": "signal", "signal": "test.answer", "key": "b"},
	}).Outcome
	if !reflect.DeepEqual(result.State.Outcome, want) {
		got, _ := json.Marshal(result.State.Outcome)
		t.Fatalf("ordered wake result = %s", got)
	}
}

func TestSourceV160RestartPreservesWakeUntilCheckpointProgress(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	type observation struct {
		Restarts *uint64
		Wake     *WakeReason
	}
	seen := make(chan observation, 2)
	observe := func(record TaskRecord) error {
		data, err := json.Marshal(record)
		if err != nil {
			return err
		}
		var persisted struct{ Restarts *uint64 }
		if err := json.Unmarshal(data, &persisted); err != nil {
			return err
		}
		seen <- observation{Restarts: persisted.Restarts, Wake: record.State.Wake}
		return nil
	}
	def, err := DefineTask(TaskDefinitionOptions{Kind: "test.recommit", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "woke"}, nil },
		Abort:   abortTestSignalTask,
		Phases: map[string]TaskPhase{
			"woke": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "woke"}}, nil
				}); err != nil {
					return err
				}
				record, found, err := r.Task(ctx, r.TaskID())
				if err != nil || !found {
					return fmt.Errorf("running reread %t: %v", found, err)
				}
				if err := observe(record); err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "next"}}, nil
				})
			},
			"next": func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
				if err := observe(record); err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.MintID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task := TaskRecord{ID: id, Conversation: 1, Kind: "test.recommit", Version: 1, Input: &TaskValue{Present: true}, State: TaskState{Status: "running", Checkpoint: JSON{"phase": "woke"}, Wake: &WakeReason{Reason: "time"}}}
	outcomes, err := store.Commit(ctx, [][]StorageWrite{{{Type: "task", Value: task}}}, nil)
	if err != nil || outcomes.Stored != 1 || outcomes.Refused != nil {
		t.Fatalf("seed crashed task: %+v, %v", outcomes, err)
	}
	h := taskTestHarness(t, store, def)
	record, found, err := h.Task(ctx, id)
	if err != nil || !found {
		t.Fatalf("recovered task %t: %v", found, err)
	}
	state, err := json.Marshal(record.State)
	const pending = `{"status":"pending","checkpoint":{"phase":"woke"},"wake":{"reason":"time"}}`
	if err != nil || string(state) != pending {
		t.Fatalf("recovered state = %s, %v; want %s", state, err, pending)
	}
	terminal, err := h.WaitForTask(ctx, id)
	if err != nil || terminal.State.Outcome == nil || terminal.State.Outcome.Status != "completed" {
		t.Fatalf("recommit did not finish: %+v, %v", terminal, err)
	}
	if len(seen) != 2 {
		t.Fatalf("recommit observations = %d; want two", len(seen))
	}
	first, next := <-seen, <-seen
	if first.Restarts == nil || *first.Restarts != 1 || !reflect.DeepEqual(first.Wake, &WakeReason{Reason: "time"}) || next.Restarts != nil || next.Wake != nil {
		t.Fatalf("same checkpoint: %+v, next: %+v; want wake and one restart only on same checkpoint", first, next)
	}
}

func TestSourceV160SignalDeliveryLifecycle(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(fmt.Sprintf("abort=%t", abort), func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(bg, 6*time.Second)
			defer cancel()
			answer := testSignalAnswer(t)
			open, err := DefineSignal(SignalDefinition{Name: "test.open", Version: 1, Schema: JSON{"type": "object", "properties": JSON{"text": JSON{"type": "string"}}, "additionalProperties": true}})
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			def := signalAskDefinition(t, answer, nil, nil)
			gate := func(next TaskPhase) TaskPhase {
				return func(ctx context.Context, record TaskRecord, r *TaskRuntime) error {
					select {
					case <-release:
						return next(ctx, record, r)
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			def.options.Phases["answered"] = gate(def.options.Phases["answered"])
			def.options.Abort = gate(def.options.Abort)
			h := taskTestHarness(t, store, def)
			cleanupTaskGates(t, release)
			id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			deliver := func(task ID, token *SignalToken, key string, value any, want SignalResult) {
				t.Helper()
				if got, err := h.Signal(ctx, task, token, key, value); err != nil || got != want {
					t.Fatalf("delivery %d %s %s: got %q, %v; want %q", task, token.Name(), key, got, err, want)
				}
			}
			if abort {
				if marked, err := h.AbortTask(ctx, id); err != nil || marked != "marked" {
					t.Fatalf("abort mark = %q, %v", marked, err)
				}
				if task, found, err := h.Task(ctx, id); err != nil || !found || !task.AbortRequested || task.State.Status == "terminal" {
					t.Fatalf("abort-marked task must still be live: %+v, %t, %v", task, found, err)
				}
				deliver(id, answer, "a", JSON{"text": "yes"}, SignalClosed)
				releaseTaskGate(release)
				if record, err := h.WaitForTask(ctx, id); err != nil || record.State.Outcome == nil || record.State.Outcome.Status != "aborted" {
					t.Fatalf("abort settlement = %+v, %v", record, err)
				}
				return
			}
			deliver(999999, answer, "a", JSON{"text": "x"}, SignalNotFound)
			deliver(id, answer, "a", JSON{"text": 1}, SignalInvalid)
			deliver(id, open, "a", JSON{"text": "x", "n": math.NaN()}, SignalInvalid)
			deliver(id, answer, "a", JSON{"text": "yes"}, SignalDelivered)
			deliver(id, answer, "a", JSON{"text": "again"}, SignalDuplicate)
			releaseTaskGate(release)
			if record, err := h.WaitForTask(ctx, id); err != nil || record.State.Outcome == nil || record.State.Outcome.Status != "completed" {
				t.Fatalf("delivery settlement = %+v, %v", record, err)
			}
			deliver(id, answer, "b", JSON{"text": "late"}, SignalClosed)
		})
	}
}
