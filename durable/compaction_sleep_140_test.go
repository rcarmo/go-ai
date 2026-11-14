package durable

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestCompaction140SleepAlarmResumesWithoutWaitAdmission(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var clock, calls atomic.Int64
		clock.Store(1000)
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			events := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				events <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
			} else {
				events <- terminal("summary")
			}
			close(events)
			return events
		})
		armed := make(chan *capturedTimer140, 8)
		options.Now = clock.Load
		options.SetTimer = func(callback func(), delay time.Duration, mode TimerOptions) func() {
			if mode.Background {
				return nil
			}
			// Once the clock reaches the deadline, the scheduler may re-arm
			// at zero while the admitted invocation is settling the task.
			if delay != 2*time.Second && !(clock.Load() == 3000 && delay == 0) {
				t.Errorf("retry sleep delay %v at clock %d", delay, clock.Load())
			}
			node := &capturedTimer140{fire: callback}
			armed <- node
			return func() { node.cancelled.Add(1) }
		}
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		if _, err := h.session.Commit(bg, func(tx *Tx) (any, error) {
			for _, text := range []string{"old", "recent"} {
				id, err := tx.mintID()
				if err != nil {
					return nil, err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return nil, err
				}
				if err = tx.appendEntryRecord(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}); err != nil {
			t.Fatal(err)
		}
		zero := 0
		configureCompactionTestPolicy(t, conversation, &zero, nil)
		id, err := conversation.Compact(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		state := waitCompactionCheckpoint(t, h, id, "retry")
		record, err := CanonicalTask(state.Tasks[id], h.session.limits)
		if err != nil || record.State.Status != "sleeping" {
			t.Fatal(record, err)
		}
		var alarm *capturedTimer140
		select {
		case alarm = <-armed:
		case <-time.After(3 * time.Second):
			t.Fatal("retry alarm missing")
		}
		if calls.Load() != 1 {
			t.Fatal("retry ran before due", calls.Load())
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		// Subscribe before firing. Observation never resumes work, so the host
		// alarm alone must admit the continuation; no polling or WaitForTask.
		_, subscription, err := h.SubscribeCommits(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer subscription.Stop()
		completed := make(chan struct{}, 1)
		if err := subscription.Start(func(_ context.Context, frame PublicationFrame) error {
			for _, write := range frame.Publication.Tables {
				if write.Task != nil && write.Task.ID == id && terminalStatus(write.Task.Status) {
					select {
					case completed <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		clock.Store(3000)
		alarm.fire()
		select {
		case <-completed:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
		state, err = h.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if state.Tasks[id].Status != "done" || calls.Load() != 2 {
			t.Fatal(state.Tasks[id].Status, calls.Load())
		}
	})
}
func TestCompaction140RetryDeadlineRejectsNonIntegerAndWideCheckpoint(t *testing.T) {
	definition := CompactionTask()
	for _, until := range []any{nil, "invalid", 1.5, json.Number("9223372036854775808")} {
		err := definition.options.Phases["retry"](bg, TaskRecord{State: TaskState{Checkpoint: JSON{"until": until}}}, nil)
		var rejected *StorageRejected
		if !errors.As(err, &rejected) {
			t.Fatal(until, err)
		}
	}
}
func TestBuiltin140PacingKeepsFullWidthClockFallback(t *testing.T) {
	for _, until := range []int64{-1, int64(MaxID) + 1, math.MaxInt64} {
		state := pacedTaskState(JSON{"phase": "retry", "until": until}, until)
		if state.Status != "running" || state.Until != nil {
			t.Fatal("full-width fallback changed", state)
		}
	}
	for _, until := range []int64{0, int64(MaxID)} {
		state := pacedTaskState(JSON{"phase": "retry", "until": until}, until)
		if state.Status != "sleeping" || state.Until == nil || *state.Until != until {
			t.Fatal(state)
		}
	}
}
