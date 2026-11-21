package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
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

func signalAskDefinition(t *testing.T, answer *SignalToken, until *int64, parked chan<- error) *TaskDefinition {
	t.Helper()
	key, err := answer.Key("a")
	if err != nil {
		t.Fatal(err)
	}
	def, err := DefineTask(TaskDefinitionOptions{Kind: "test.ask", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "ask"}, nil },
		Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted", Reason: "test"}}, nil
			})
		},
		Phases: map[string]TaskPhase{
			"ask": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "answered"}, Until: until, Signals: []SignalKey{key}}, nil
				})
				if parked != nil {
					parked <- err
				}
				return err
			},
			"answered": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
				state, err := r.ReadSignal(ctx, answer, "a")
				if err != nil {
					return err
				}
				raw, err := json.Marshal(JSON{"wake": task.State.Wake, "states": []*SignalState{state}})
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
	h := taskTestHarness(t, store, child, parent)
	value, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.CreateTask(parent, nil, TaskOptions{Conversation: 1, Ownership: TaskOwnership{Kind: "conversation"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	id := value.(ID)
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
	first, second := task.State.On[0], task.State.On[1]
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
