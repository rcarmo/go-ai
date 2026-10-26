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
	backends(t, func(t *testing.T, b backend) {
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
		if _, err := h.session.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		zero := 0
		id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokensOverride: &zero})
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
		clock.Store(3000)
		alarm.fire()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		// Only observational reads here: WaitForTask would itself Resume and mask a
		// lost deadline wakeup. The host alarm alone must admit the continuation.
		for {
			state, err = h.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if terminalStatus(state.Tasks[id].Status) {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(time.Millisecond):
			}
		}
		if state.Tasks[id].Status != "done" || calls.Load() != 2 {
			t.Fatal(state.Tasks[id].Status, calls.Load())
		}
	})
}
func TestCompaction140RetryDeadlineRejectsNonIntegerAndWideCheckpoint(t *testing.T) {
	definition, err := (&Harness{}).compactionDefinition()
	if err != nil {
		t.Fatal(err)
	}
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
