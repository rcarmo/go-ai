package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// The read projection is not a memo format change. Deliver through the public
// API, reopen the same SQLite task with its legacy memo objects, and check that
// the view distinguishes no delivery, explicit null, a value, and expiration.
func TestV161SignalReadKeepsLegacyMemosAndDetachedValues(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	token, err := DefineSignal(SignalDefinition{Name: "test.tagged", Version: 1, Schema: JSON{"type": []any{"object", "null"}}})
	if err != nil {
		t.Fatal(err)
	}
	deadlineKey, err := token.Key("expiry")
	if err != nil {
		t.Fatal(err)
	}
	until := int64(2000)
	var clock atomic.Int64
	clock.Store(1000)
	parked := make(chan error, 1)
	wantValue := JSON{"detail": JSON{"text": "untouched"}}
	const wantRead = `[null,{"kind":"value","value":null},{"kind":"value","value":{"detail":{"text":"untouched"}}},{"kind":"expired"}]`
	checkMemos := func(memos map[string]*TaskValue, expired bool) error {
		want := map[string]JSON{
			signalMemoName(token.Name(), "null"):   {"version": 1, "value": nil},
			signalMemoName(token.Name(), "object"): {"version": 1, "value": wantValue},
		}
		if expired {
			want[signalMemoName(token.Name(), "expiry")] = JSON{"expired": true}
		}
		if len(memos) != len(want) {
			return fmt.Errorf("stored memo count = %d, want %d", len(memos), len(want))
		}
		for key, value := range want {
			if memo := memos[key]; memo == nil || !memo.Present || !equalJSONValue(memo.Value, value) {
				return fmt.Errorf("stored memo %s = %+v, want %v", key, memo, value)
			}
		}
		return nil
	}
	def, err := DefineTask(TaskDefinitionOptions{
		Kind: "test.tagged-signal-reader", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "ask"}, nil }, Abort: abortTestSignalTask,
		Phases: map[string]TaskPhase{
			"ask": func(run context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "sleeping", Until: &until, Signals: []SignalKey{deadlineKey}, Checkpoint: JSON{"phase": "read"}}, nil
				})
				parked <- err
				return err
			},
			"read": func(run context.Context, task TaskRecord, r *TaskRuntime) error {
				if err := checkMemos(task.Memos, true); err != nil {
					return err
				}
				first, err := r.ReadSignal(run, token, "object")
				if err != nil || first == nil || first.Expired || !equalJSONValue(first.Value, wantValue) {
					return fmt.Errorf("initial object read = %+v, %v", first, err)
				}
				generationObject(generationObject(first.Value)["detail"])["text"] = "edited by the reader"
				var states []*SignalState
				for _, key := range []string{"absent", "null", "object", "expiry"} {
					view, err := r.ReadSignal(run, token, key)
					if err != nil {
						return err
					}
					states = append(states, view)
				}
				// Check the Go view, independently of its caller-facing JSON.
				if states[0] != nil || states[1] == nil || states[1].Expired || states[1].Value != nil || states[2] == nil || states[2].Expired || !equalJSONValue(states[2].Value, wantValue) || states[3] == nil || !states[3].Expired {
					return fmt.Errorf("wrong distinguishable Go signal states: %v", states)
				}
				wire, err := json.Marshal(states)
				if err != nil {
					return err
				}
				return r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(string(wire)), nil })
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := taskTestHarnessOptions(t, store, Options{Now: clock.Load, OnWake: func(Wake) {}}, def)
	id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	delivered := JSON{"detail": JSON{"text": "untouched"}}
	result, err := h.Commit(ctx, func(tx *Tx) (any, error) {
		return tx.Signals(SignalDelivery{id, token, "null", nil}, SignalDelivery{id, token, "object", delivered})
	})
	if results, ok := result.([]SignalResult); err != nil || !ok || len(results) != 2 || results[0] != SignalDelivered || results[1] != SignalDelivered {
		t.Fatal("could not deliver exact legacy memos", result, err)
	}
	generationObject(delivered["detail"])["text"] = "edited by the sender"
	if record, found, err := store.Task(ctx, id); err != nil || !found || record.State.Status != "pending" {
		t.Fatal("expected actual persisted pending task before reopen", record, found, err)
	} else if err := checkMemos(record.Memos, false); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h = openHarness(t, reopenStoreAfterHarnessClose(t, store), h.options)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if err := watchReceive(t, parked); err != nil {
		t.Fatal(err)
	}
	result, err = h.Commit(ctx, func(tx *Tx) (any, error) {
		clock.Store(2500)
		return tx.Signal(id, token, "expiry", nil)
	})
	if err != nil || result != SignalClosed {
		t.Fatal("expiry delivery should close before reservation", result, err)
	}
	record, err := h.WaitForTask(ctx, id)
	if err != nil || record.State.Outcome == nil || record.State.Outcome.Status != "completed" || record.State.Outcome.Result == nil || !record.State.Outcome.Result.Present || record.State.Outcome.Result.Value != wantRead {
		t.Fatal("tagged read through reopened legacy memos", record.State.Outcome, err)
	}
}
